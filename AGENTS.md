# codetospec — guide agent

Binaire Go qui lit un dépôt et produit un graphe markdown de règles métier
(EARS), citées ligne à ligne et vérifiées. Pour le quoi/pourquoi, voir
`README.md`. Ici : comment travailler dans le repo.

## Commandes

```sh
go build -o bin/codetospec ./cmd/codetospec   # TOUJOURS rebuild avant un run réel
make test                                      # go vet + go test, tous les modules
make check                                     # lint + test + race + tidy + vuln (= CI)
make corpus                                    # graphes réels vs baselines, sans LLM
make run-fixture                               # run mocké sur testdata/fixture
```

- Lint : `.golangci.yml` du dépôt (copie de `~/Documents/git/.golangci.yml`),
  golangci-lint ≥ v2.14 (2.13 ne lit pas l'export data de go1.27.2). Ne
  jamais le relâcher ; un `//nolint:<linter> // <raison>` seulement quand le
  code est juste par conception (nolintlint exige la raison).
- `make corpus` (`internal/corpus`, tag `corpus`) rejoue grounding, verify et
  drift sur les graphes de `internal/corpus/testdata/corpus.json` (sorties
  locales sous `~/spec-graphs`) et échoue si un compte empire ; une entrée
  absente ou dont la source a quitté le commit enregistré est sautée.
  `CODETOSPEC_CORPUS_UPDATE=1` ré-enregistre les baselines (après revue).
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
  Piège : `Merge` déduplique par ID et un fait `module` a l'ID
  `module.<nom>` — il n'en reste qu'un par nom. Tout calcul « par fichier »
  (ex. `AmbiguousNamespaces`, les `main` Go de chaque `cmd/`) lit donc le
  `fileInfo` tree-sitter, pas les faits fusionnés ; le tester par le vrai
  chemin (`sitter.Parse` → `UniversalFacts` → `Merge`), pas avec des faits
  construits à la main.
- `internal/{mapper,reducer,crosscheck}` — phases LLM, chacune valide
  mécaniquement sa sortie (2 corrections max puis échec tracé). Le map préfixe
  chaque ligne du code de son numéro absolu (`numberedContent`) : sinon le
  modèle *compte* les lignes et dérive (citations off-by-N sur gros chunks). Le crosscheck
  (`--crosscheck`) réfute chaque règle ; avec `--repair` (+ index SCIP), une
  règle rejetée re-cite le span exact d'un symbole précis. La citation doit
  chevaucher un vrai corps de symbole (grounding mécanique), puis elle est
  re-revue à l'aveugle (`review` ne reçoit ni le premier verdict ni sa
  raison). Elle n'est adoptée que si cette revue dit `supported` ; sinon la
  règle garde son verdict (`repair_review` le trace).
- `internal/{graph,verify,render}` — assemblage déterministe, contrôles, écriture.
- `internal/drift` — digest du code cité par règle (frontmatter `digest:`) ;
  commande `drift` = re-hache et signale les règles périmées (déterministe).
  Avec des faits `call`, `stamp` écrit aussi les appelés directs du code
  cité (`callees:` + `callees_digest`, un seul saut : la fermeture couvrirait
  tout le module) ; un appelé modifié rend la règle `affected`, signalé mais
  jamais en échec. Autonome : `drift` ne lit pas `.codetospec/`.
- `graph.AnnotateCoverage` — `coverage: complete | partial` par domaine, avec
  `coverage_gaps` (fichier généré, chunk map en échec, reduce en échec),
  calculé par `coverageGaps` dans `pipeline.go` ; section « Code sans règle »
  du README de sortie. Un chemin exclu ou sans grammaire n'a pas de domaine :
  il ne rend jamais un domaine partiel.
- `internal/atomicfs` — seules écritures de la sortie : `WriteFile` (temp +
  rename) et `ReplaceDir` (`nodes/`, `models/` construits à côté puis
  échangés) ; un rendu interrompu laisse la sortie précédente intacte.
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
  à tort toute règle anglaise dont le code contient un `1`. Le map
  l'applique aussi en cours de route (`mapper.Ungrounded`, injecté par
  `main` pour éviter le cycle grounding → graph → mapper) : une réponse aux
  règles non fondées reçoit un échange de correction, adopté seulement s'il
  est meilleur (jamais pire ; `mapper/ground.go`). Ce texte de correction
  n'entre pas dans `promptKey` : le cache map reste valide ; `--reground`
  rejoue l'échange sur les sorties en cache, une fois chacune (champ
  `regrounded` de la sortie). Une citation seulement trop étroite est
  élargie sans LLM (`grounding.Annotate`, `widening`) : valeur présente à
  une seule autre ligne du fichier, à ≤ 30 lignes, pas un chiffre seul ;
  tracée dans `grounding_widened`.
- `internal/graph/handlers.go` — lien mécanique règle → endpoint
  (`exposed_by`) quand une citation recouvre le code du handler
  (`handler_path` + `handler_lines` d'un fait `route`) : tient sur cache
  chaud, où le map n'est jamais re-demandé (les listes d'endpoints autorisés
  ne sont pas dans `promptKey`).
- `extract.Roots` — seule lecture des fichiers cités : `--src`, plus les
  arbres `--ref-root nom=dir` qu'une citation nomme (`Ref.Root` : le code
  partagé, `pkg` pour webapp et districts-jobs). Une racine citée sans
  `--ref-root` arrête le run avant tout appel LLM (`Roots.CheckRoots`). Grounding, drift,
  verify passent par elle ; `Ref.File()` (`@nom/chemin`) sert de clé et
  d'affichage. Un `Root` vide ne change rien (digests, goldens, caches).
  Une règle d'extracteur peut être **héritée** d'une autre spec
  (`inherited_from` + son verdict) : `graph.Reviewable` l'écarte du
  crosscheck, le rendu n'écrit jamais `extracted_by` pour elle.
- `internal/communities` — domaines « dispersés » : communautés du graphe
  d'imports (Louvain de gonum + composantes connexes, graine fixe), section
  du README de sortie. gonum est épinglé : une montée de version peut
  changer les communautés.
- `internal/inherit` — hérite d'une spec de code partagé (`--inherit
  nom=graphe`, exige `--ref-root nom=dir`) : chaque règle LLM à une seule
  citation tombant dans du code atteint (faits `reach`, émis par un
  extracteur, ex. `extractors/go -shared pkg=…`) devient une règle héritée
  du domaine `shared-<domaine>`. Le cœur lit son propre format de spec ;
  l'extracteur ne dit que ce qui est atteint. `scandistricts.sh` l'applique
  à districts-jobs, `scanwebapp.sh` à webapp (faits `reach` de l'extracteur
  webapp).
- `internal/callers` — après le chunking, un chunk sans fait `domain`
  prend le domaine sur lequel s'accordent ses appelants **du même
  répertoire** (faits `call`, émis par l'extracteur Go), appelants décidés
  d'abord (ordre de Kahn, prêts triés : déterministe) ; un cycle d'appels
  garde ses domaines. Un appelant d'un autre répertoire est ignoré : sinon
  le code de bibliothèque prend le domaine `main` de son seul appelant, et
  les `models` partagés celui de `servicetest` (les vrais appelants sont
  dans d'autres modules).
- `internal/consistency` — détection de règles quasi-doublons (Jaccard sur
  les exigences), listées dans le README de sortie.
- `graph.SharedEntities` — entités touchées par des règles de plusieurs
  domaines (liens `touches` du reduce), section « Entités partagées » du
  README de sortie : noyau partagé ou frontière mal placée.
- `internal/domainmodel` — comparaison avec des modèles modelith écrits
  par des humains (`--domain-model`, lecture seule, sans LLM), via
  `github.com/StevenACoffman/indomain` (fork importable de modelith, admis
  dans le module principal). Lint d'abord : une erreur arrête le run avant
  tout appel LLM. Entités appariées par nom (normalisé, pluriel régulier
  accepté, ou `domain_model_entities`), invariants ↔ règles qui touchent
  l'entité par Jaccard (seuil 0,3, mots des patrons EARS retirés) ; section
  du README de sortie. Hors `promptKey` : aucun cache invalidé.
  `--write-domain-model` : `domainmodel.Enhance` (pur, renvoie les
  fichiers) écrit sous `<out>/models/` les modèles humains enrichis
  (nœuds yaml.v3 : commentaires et ordre gardés ; définitions E → C → A,
  le préfixe du placeholder le fait reconnaître au tour suivant) et un
  `generated/<domaine>.modelith.yaml` par domaine pour les entités sans
  modèle, plus `provenance.json`. Un modèle écrit relu comme humain ne
  change pas (test d'aller-retour).
- `cmd/codetospec/main.go` — séquence complète.

## Extracteurs = modules Go SÉPARÉS

`extractors/php/` (composer), `extractors/go/` (`go.mod` propre, dépend de
`golang.org/x/tools`), `extractors/scip/` (dépend de `github.com/scip-code/scip`
+ protobuf) et `extractors/webapp/` (Khan webapp/gqlgen, dépend de
`vektah/gqlparser/v2` + yaml) **ne sont pas** construits par `go build ./...` depuis la racine.
Les tester/builder depuis leur dossier. Raison : le module principal a un jeu
de dépendances verrouillé (yaml, tree-sitter, charm) — les extracteurs vivent
hors de ce jeu. Le convertisseur SCIP et l'extracteur Go émettent des
facts `symbol` avec `precise: true` (mêmes IDs, ils fusionnent) que le map
injecte comme ancres de citation ; pour Go, SCIP est donc facultatif
(`SCIP=0` par défaut dans les scripts). L'extracteur Go émet aussi les
faits `constant` (webapp n'en émet plus) et `call`.

Faits `constant` (grounding : `400` ↔ `http.StatusBadRequest`), `doc`
(section `SCHEMA_DOCS` du map), `dependency` (nœuds `service` +
`depends_on` ; `as` = `service-admin` | `user`, l'autorité de l'appel) et
`route.handler_path` complètent le contrat. Le reduce a un
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
restantes sont réutilisées. `state.json` cumule les tokens entre runs et
enregistre la source (chemin absolu) : `state.Open` refuse un `--out` qui
appartient à une autre source (`state.ErrOtherSource`), avant toute
écriture — `start` ouvre l'état avant d'écrire `facts.json`.

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
- Pas de logger global : `main` construit un `*slog.Logger` (`newLogger`) et
  le passe explicitement (`runPipeline`, champs `Log` de `Mapper`,
  `Reducer`, `Checker`, argument de `extract.RunExtractor`, `ui.Plain`).
  Jamais `slog.SetDefault` ni `slog.Info` ; les tests passent un logger sur
  `t.Output()`.
- Golden tests render : `UPDATE_GOLDEN=1 go test ./internal/render` pour régénérer (pas de flag global ; idem `internal/domainmodel` et `extractors/webapp`).
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
