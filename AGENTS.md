# codetospec — guide agent

Binaire Go qui lit un dépôt et produit un graphe markdown de règles métier
(EARS), citées ligne à ligne et vérifiées. Pour le quoi/pourquoi, voir
`README.md`. Ici : comment travailler dans le repo.

## Commandes

```sh
go build -o bin/codetospec ./cmd/codetospec   # TOUJOURS rebuild avant un run réel
make test                                      # go vet ./... && go test ./...
make run-fixture                               # run mocké sur testdata/fixture
```

- **Rebuild `bin/codetospec` après toute modif de code avant de le lancer** —
  un binaire périmé produit un run faux silencieusement (déjà arrivé).
- CGO obligatoire (tree-sitter) : `CGO_ENABLED=1`, toolchain C requise.
- Mesure du crosscheck LLM sur des fautes plantées (opt-in, appelle le
  modèle) : `CODETOSPEC_EVAL=1 go test ./internal/crosscheck -run Eval -v`
  (avec `LLM_BASE_URL`, `LLM_MODEL`). À relancer avant d'adopter un nouveau
  prompt ou modèle. `TestPlantedFaults` (grounding) est l'équivalent
  mécanique, toujours actif, angles morts listés explicitement.
- `go test ./...` tourne sans réseau ni PHP. Tests taggés `phplocal` pour
  l'extracteur PHP (skip si `php` absent).

## Architecture

Pipeline : `extract → chunk → map → reduce → crosscheck(+repair) → build → digest → verify → render`.
- `internal/sitter` — tree-sitter, une grammaire + un `.scm` par langage.
  Le chunker regroupe les petites définitions adjacentes (même fichier,
  namespace et domaine) jusqu'à 150 lignes (`pack`) : le coût fixe d'un
  appel map (~570 tokens) domine les petites fonctions.
  Ajouter un langage = grammaire + fichier query, zéro modif du cœur.
- `internal/extract` — modèle Fact, fusion, protocole extracteurs externes.
- `internal/{mapper,reducer,crosscheck}` — phases LLM, chacune valide
  mécaniquement sa sortie (2 corrections max puis échec tracé). Le map préfixe
  chaque ligne du code de son numéro absolu (`numberedContent`) : sinon le
  modèle *compte* les lignes et dérive (citations off-by-N sur gros chunks). Le crosscheck
  (`--crosscheck`) réfute chaque règle ; avec `--repair` (+ index SCIP), une
  règle rejetée re-cite le span exact d'un symbole précis, adopté seulement
  s'il chevauche un vrai corps de symbole (grounding mécanique).
- `internal/{graph,verify,render}` — assemblage déterministe, contrôles, écriture.
- `internal/drift` — digest du code cité par règle (frontmatter `digest:`) ;
  commande `drift` = re-hache et signale les règles périmées (déterministe).
- `internal/locale` — toutes les chaînes fixes de sortie, FR et EN côte à
  côte (`tr(fr, en)`), y compris les patrons EARS des prompts. Les écrivains
  prennent `locale.For(--lang)` ; les parseurs (`graph.RuleRequirement`,
  `graph.RuleCriteria`) acceptent les libellés de toutes les langues.
- `internal/grounding` — contrôle mécanique (sans LLM) : chaque nombre /
  chaîne citée de l'exigence EARS doit apparaître dans les lignes citées.
  `grounded | ungrounded | unchecked` en frontmatter ; « unchecked » n'est
  jamais un succès. Seule l'exigence est vérifiée (les critères d'acceptation
  contiennent des exemples de test absents du code). Les nombres sont lus
  avec le séparateur décimal de `--lang` (`locale.Text.DecimalSeparator`) :
  une seule lecture, jamais les deux — accepter « 1,000 = 1 » ferait passer
  à tort toute règle anglaise dont le code contient un `1`.
- `internal/consistency` — détection de règles quasi-doublons (Jaccard sur
  les exigences), listées dans le README de sortie.
- `cmd/codetospec/main.go` — séquence complète.

## Extracteurs = modules Go SÉPARÉS

`extractors/php/` (composer), `extractors/go/` (`go.mod` propre, dépend de
`golang.org/x/tools`), `extractors/scip/` (dépend de `github.com/scip-code/scip`
+ protobuf) et `extractors/webapp/` (Khan webapp/gqlgen, dépend de
`vektah/gqlparser/v2` + yaml) **ne sont pas** construits par `go build ./...` depuis la racine.
Les tester/builder depuis leur dossier. Raison : le module principal a un jeu
de dépendances verrouillé (yaml, tree-sitter, charm) — les extracteurs vivent
hors de ce jeu. Le convertisseur SCIP émet des facts `symbol` avec
`precise: true` que le map injecte comme ancres de citation.

Faits `constant` (grounding : `400` ↔ `http.StatusBadRequest`), `doc`
(section `SCHEMA_DOCS` du map), `dependency` (nœuds `service` +
`depends_on`) et `route.handler_path` complètent le contrat. Le reduce a un
mode `--reduce-mode merge` (`reducer.ModeMerge`) : seuls les candidats en
cluster (Jaccard ≥ 0,6 ou citations chevauchantes, `internal/textsim`) vont
au LLM.

Faits `rule` et `domain` (contrat : constantes de `internal/extract`) : un
extracteur peut fournir des règles déterministes (validées par
`graph.ValidateRuleFacts`, rendues `extracted_by` + `crosscheck: mechanical`,
listées au map en `MECHANICAL_RULES`) et des domaines par plage de lignes
(`extract.DomainHints`, prioritaires sur la stratégie de domaine). Aucun code
spécifique à un dépôt dans le cœur : tout le savoir webapp vit dans
`extractors/webapp`.

## Cache & reprise (`<out>/.codetospec/`)

- `map/<chunkID>.json` — nom = hash du contenu du chunk ; réutilisé si son `prompt_key` correspond. Le domaine d'une sortie en cache est repris du chunk courant (changer la stratégie de domaine regroupe sans re-mapper).
- `reduce/<domain>.json` — réutilisé si le hash des candidates (`candidates_hash`) correspond.
- `crosscheck/<ruleID>.json` — clé = hash (prompt key + règle + lignes citées).

Les trois clés incluent `llm.PromptKey` : modèle, langue et **texte des
templates de prompt** (+ `--repair` pour le crosscheck). Éditer un prompt ou
changer de modèle invalide donc le cache tout seul — aucune constante de
version à bumper. Un cache sans clé (version antérieure) est considéré
périmé.

Reprise = existence de ces fichiers. Pour rejouer une phase, **purger son
cache** (`rm -rf .codetospec/reduce`) puis relancer ; les phases amont
restantes sont réutilisées. `state.json` cumule les tokens entre runs.

Les trois phases sont *content-aware* : map (clé = hash contenu du chunk),
reduce (clé = hash des candidates du domaine, stocké dans le `.json`) et
crosscheck (clé = hash règle+lignes). Après une édition du code, le re-run
refait automatiquement ce qui a changé — plus de purge manuelle du reduce.
Le reduce tourne aussi en worker pool (`--workers`), les domaines étant
indépendants (le `depends_on` est calculé au build).

## Conventions

- Sortie déterministe : tris stables partout, deux runs cache chaud =
  identiques octet pour octet. Ne jamais introduire d'ordre non déterministe.
- Code/commentaires/identifiants en **anglais** ; comms utilisateur en français.
- Golden tests render : `go test ./internal/render -update` pour régénérer.
- README tenu à jour à chaque feature livrée (concis).

## Notes

Le reduce découpe automatiquement les domaines de plus de `--reduce-batch`
candidates (défaut 30) en lots fusionnés déterministiquement ; un lot qui
tronque encore est re-découpé en deux et réessayé (halving rapide, sans
corrections gaspillées) jusqu'à un plancher. Un gros domaine ne perd plus ses
règles. La fusion inter-lots déduplique par requirement exact et renumérote
les slugs en collision (pas de dédup sémantique fine entre lots : limite
assumée). Attention : un domaine à plusieurs centaines de candidates (ex. des
resolvers GraphQL) reste lent — souvent de la plomberie à exclure en amont.
