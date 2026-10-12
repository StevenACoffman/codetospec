# Extracteur webapp (Khan Academy)

Extracteur codetospec pour les services Go de `Khan/webapp` (GraphQL via
gqlgen), sur le protocole `codetospec/facts/v1`. Il émet, sans LLM, ce qu'un
modèle reproduit cher et approximativement, ou pas du tout :

- **Règles d'autorisation** (`rule`) : le garde d'accès de chaque méthode de
  resolver gqlgen (`acl.IsLoggedIn`, `acl.IsCurrentUser`,
  `acl.ActorHasPermission[Err]`, `acl.IsCoachedByActor`,
  `ktx.RequestIs…()`, et le marqueur `acl.OpenAccess()`), rendu en exigence
  EARS qui **nomme l'opération GraphQL** (`Mutation.addAuthEmail`), citée sur
  les lignes exactes du garde. Formes reconnues : `if <prédicat> { … return … }`
  au niveau supérieur de la méthode, prédicat direct ou via une variable
  (`allowed := acl.IsLoggedIn(...)`, `ok, err := acl.ActorHasPermissionErr(...)`).
  Un prédicat inconnu est cité tel quel (jamais paraphrasé) ; une forme non
  reconnue ne produit **aucune** règle — le LLM la couvre.
- **Domaines** (`domain`) : chaque méthode de resolver est rattachée au domaine
  du fichier de schéma qui déclare son champ (`auth_emails.graphql` →
  `auth-emails`), d'après `gqlgen.yml` et `vektah/gqlparser/v2` (le parseur de
  gqlgen). Les schémas partagés hors service (`../../pkg/...`) sont ignorés.
- **Endpoints** (`route`) : chaque champ racine (`Query`, `Mutation`,
  `Subscription`) qui a une méthode de resolver, avec `handler_path` ; les
  règles d'autorisation y sont reliées (`exposed_by`).
- **Entités** (`table`) : chaque `datastore.RegisterKind("Kind", new(T))`,
  colonnes = champs de `T` tels que Datastore les stocke (tag `datastore`).
- **Constantes** (`constant`) : constantes numériques du service et
  `http.Status*` (lus dans GOROOT), pour le grounding.
- **Docs** (`doc`) : description GraphQL du champ et de ses arguments,
  montrée au map pour la méthode qui le résout.
- **Appels inter-services** (`dependency`) : chaque appel
  `genqlient.<Op>(…)` dont un champ racine appartient à un autre subgraph
  (`@join__field` du schéma composé `gengraphql/composed_schema.graphql`).
  Options `-supergraph` (dossier du schéma composé, défaut
  `<root>/../../gengraphql`) et `-graph` (subgraph du service, défaut : nom
  du dossier en majuscules).

## Usage

```sh
go build -o extract .
./extract -root ~/khan/webapp/services/users -lang en
```

```yaml
# codetospec.yaml
extractors:
  - name: webapp
    cmd: /chemin/vers/extractors/webapp/extract
    args: [-root, "{src}", -lang, "{lang}"]
```

`--src` de codetospec doit être le dossier du service (celui qui contient
`gqlgen.yml` et `resolvers/`). Les avertissements (fichier non parsable,
méthode sans champ de schéma) vont sur stderr.

## Tests

```sh
go test ./...                    # golden testdata/golden.{fr,en}.json
UPDATE_GOLDEN=1 go test ./...    # régénérer, puis relire le diff
```
