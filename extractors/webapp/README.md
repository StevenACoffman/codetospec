# Extracteur webapp (Khan Academy)

Extracteur codetospec pour les services Go de `Khan/webapp` (GraphQL via
gqlgen), sur le protocole `codetospec/facts/v1`. Il émet, sans LLM, ce qu'un
modèle reproduit cher et approximativement, ou pas du tout :

- **Règles d'autorisation** (`rule`) : le garde d'accès de chaque méthode de
  resolver gqlgen (`acl.IsLoggedIn`, `acl.IsCurrentUser`,
  `acl.ActorHasPermission[Err]`, `acl.IsCoachedByActor`,
  `ktx.RequestIs…()`, les prédicats du paquet d'accès du service — fonctions
  exportées de `permissions/` dont le premier résultat est un `bool`, option
  `-access-pkgs` ; les chargeurs comme `GetAllVisibleUDIs` n'en sont pas —
  et le marqueur `acl.OpenAccess()`), rendu en exigence
  EARS qui **nomme l'opération GraphQL** (`Mutation.addAuthEmail`), citée sur
  les lignes exactes du garde. Formes reconnues, au niveau supérieur de la
  méthode : `if <prédicat> { … return … }`, prédicat direct, via une variable
  (`allowed := acl.IsLoggedIn(...)`, `ok, err := acl.ActorHasPermissionErr(...)`)
  ou via un prédicat auxiliaire du paquet (`func _isActor(…) bool { return
  acl.IsCurrentUser(…) }`) ; et tout `if` dont le bloc renvoie une erreur
  d'autorisation (`errors.Unauthorized`, `NotAllowed`, `Forbidden`,
  `PermissionDenied`) sur son propre chemin, quelle que soit sa condition
  (`if underAgeGate {…}`) : une erreur dans un `if`/`switch` imbriqué est un
  autre chemin (`if ok { …; if err != nil { return Unauthorized }; return
  result }` réussit et n'est pas un garde ; seul l'`if` imbriqué l'est).
  Le bloc doit **finir** par un `return` : un bloc qui retombe dans la suite
  (`if hasAny { … if err != nil { return … } … }`) n'est pas un garde. Un
  garde imbriqué d'un niveau (`if C1 { …; if C2 { … return … } }`, sans
  `else`) devient « C1 ET C2 », cité depuis le `if` externe. L'erreur d'un
  appel (`ok, err := f(…)`) se lit « `f(…)` échoue » dans `err != nil`, mais
  ne fait jamais seule un garde (`if err != nil { return nil, err }` est de
  la plomberie) ; une variable réaffectée dans un bloc imbriqué n'est plus
  rattachée à son appel. Le résultat est nommé : rejet avec l'erreur `X`, ou
  « ne renvoie aucune donnée » quand le garde renvoie des valeurs nulles
  (`return nil, nil`). Un prédicat inconnu est cité tel quel (jamais
  paraphrasé) ; une forme non reconnue ne produit **aucune** règle : le LLM
  la couvre.
- **Domaines** (`domain`) : chaque méthode de resolver est rattachée au domaine
  du fichier de schéma qui déclare son champ (`auth_emails.graphql` →
  `auth-emails`), d'après `gqlgen.yml` et `vektah/gqlparser/v2` (le parseur de
  gqlgen). Les schémas partagés hors service (`../../pkg/...`) sont ignorés.
  Un schéma fourre-tout — plus de la moitié des méthodes, réparties sur au
  moins 10 fichiers (le `schema.graphql` de districts) — ne nomme pas de
  domaine : ses méthodes prennent celui de leur fichier de resolvers
  (`user_district_info.go` → `user-district-info`).
  Un fichier de resolvers dont toutes les méthodes ont le même domaine le
  donne au fichier entier, pour ses fonctions auxiliaires.
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
