# Extracteur webapp (Khan Academy)

Extracteur codetospec pour les services Go de `Khan/webapp` (GraphQL via
gqlgen), sur le protocole `codetospec/facts/v1`. Il émet, sans LLM, ce qu'un
modèle reproduit cher et approximativement, ou pas du tout :

- **Règles d'autorisation** (`rule`) : le garde d'accès de chaque méthode de
  resolver gqlgen. Un contrôle est d'abord ce que webapp déclare comme tel :
  toute fonction, méthode d'interface ou variable marquée
  `//ka:permission-check` dans le service (la directive qu'exige son linter
  `dev/linters/permissions_lint.go`, ADR-211), et ceux que marquent les paquets
  partagés de webapp (`pkg/khan/acl`, `pkg/web` : option `-webapp`, par défaut
  `<root>/../..` ; seuls les fichiers de `pkg/` portant la directive sont lus,
  le service garde la main sur ses propres marquages). L'erreur d'un tel contrôle
  est un refus seulement s'il construit lui-même une erreur d'autorisation
  (`requireLoggedInAdminKaid` → `errors.Unauthorized`) ; sinon c'est un échec
  de lecture (`GetAllVisibleUDIs` refuse par une liste vide,
  `UserIsKADSSSuperAdmin` par `false`) et `if err != nil` reste de la
  plomberie. Forme `if err := check(); err != nil {…}` reconnue. S'y ajoutent
  (`acl.IsLoggedIn`, `acl.IsCurrentUser`,
  `acl.ActorHasPermission[Err]`, `acl.IsCoachedByActor`,
  `ktx.RequestIs…()`, les prédicats du paquet d'accès du service — fonctions
  exportées de `permissions/` dont le premier résultat est un `bool`, option
  `-access-pkgs` ; les chargeurs comme `GetAllVisibleUDIs` n'en sont pas —
  et le marqueur `acl.OpenAccess()`), rendu en exigence
  EARS qui **nomme l'opération GraphQL** (`Mutation.addAuthEmail`), citée sur
  les lignes exactes du garde. Le corps est parcouru branche par branche
  (`if`/`else`, `else if`, `switch` sans étiquette, boucles `range`, jusqu'à
  4 niveaux) : la condition d'un garde est celle des branches qui y mènent
  ET la sienne (« `kaid == nil` est faux ET l'appelant n'est pas
  l'utilisateur `kaid` »), le `default` d'un `switch` vaut « aucun des cas »,
  et un `else if` après une branche qui rend se lit comme l'instruction
  suivante. Un garde est un bloc qui **finit** par un `return` (ou, dans une
  boucle, par `continue` : « rejeter cet élément `udiID` » ; un `return` dans
  une boucle se lit « pour un `cdi` de `cdis` ») et dont la condition propre
  utilise un contrôle, ou qui renvoie une erreur d'autorisation sur son
  propre chemin : `errors.Unauthorized`/`NotAllowed`/`Forbidden`/
  `PermissionDenied`, ou un code GraphQL `…ErrorCodeUnauthorized`. Les
  variables sont remplacées par ce qu'elles valent, appel ou condition
  (`authorized := a || b` puis `if !authorized`), y compris l'idiome
  `ok := A; if !ok { ok = B }` (« A OU B ») ; une variable réaffectée dans un
  bloc imbriqué n'est plus rattachée. L'erreur d'un appel se lit « `f(…)`
  échoue », et ne fait un garde que si l'appel refuse par son erreur — lui
  ou, par délégation, une fonction qu'il appelle (`updateUserRoleValidation`
  → `getValidatedAdminKaid`). `acl.IsCurrentUser` sur le kaid de l'appelant
  lui-même (`kaid := user.Kaid`, `user` de `RequestUser()`) est signalé
  « (jamais vrai) » : ce contrôle ne refuse personne. Le résultat est nommé :
  rejet avec l'erreur `X`, ou « ne renvoie aucune donnée » quand le garde
  renvoie des valeurs nulles ou vides (`return nil, nil`, `[]T{}`). Un
  prédicat inconnu est cité tel quel (jamais paraphrasé).
- **Visibilité** (`rule`, « Visibilité : … ») : chaque appel d'un filtre
  marqué — premier résultat une collection, erreur qui ne refuse pas
  (`permissions.GetAllVisibleUDIs`) — limite l'opération à ce que le filtre
  laisse voir à l'appelant.
- **Accès non vérifié** (`rule`, « Accès non vérifié : … ») : `acl.TODO()`,
  que webapp documente comme un contrôle vide marquant un resolver sans
  vérification de permission (« un risque de sécurité potentiel »).
- **Rôles** (`rule`, « Rôle : … », service admin) : chaque
  `RegisterRoleDefinition(RoleDefinition{…})` accorde ses capacités —
  « Le systeme doit accorder au rôle `supporter` (…) les capacités
  `CanViewUserIdentity`, … » ; attributs `role` et `capabilities` pour la
  jointure avec les permissions que les services vérifient.
- **Vérification d'accès** (`rule`, « Vérification d'accès : … ») : un
  resolver sans autre règle nomme le premier contrôle qu'il référence — le
  linter de webapp en exige un — sans prétendre dire comment il décide. À
  défaut, un alias nomme la méthode exportée du même resolver qu'il appelle
  (`FindManyStudentListByDescriptors` → `r.FindManyClassroomByDescriptors`),
  y compris sur un récepteur reconstruit
  (`(&mutationResolver{r.Resolver}).SyncActiveAssignmentsInClassroomToLms`),
  seulement si celle-ci a une règle.
  Restent à ce niveau les formes qui demandent un flot de données : erreur
  différée (`err = errors.Unauthorized()` rendue plus loin), erreurs
  chaînées (`if err == nil { err = check() }`), branche d'octroi
  (`if canView { result = … }`).
- **Handlers net/http** (`rule`, « Contrôle d'accès : /safelist/add ») :
  chaque handler enregistré par `serve.HandleFunc(path, h)` /
  `serve.Handle(path, h)` hors de `resolvers/` — valeur de méthode
  (`rts.HandleGraphQL`), fonction, closure qui n'appelle qu'elle, ou
  enveloppe (`serve.CreateTaskHandler(f)`) — est parcouru comme un resolver ;
  la règle nomme ses chemins. C'est le cas du graphql-gateway, qui n'a ni
  `gqlgen.yml` ni resolvers. Un refus s'y lit aussi par le statut écrit
  (`w.WriteHeader(http.StatusUnauthorized)`, `http.Error(…, 403)`) ; une
  erreur construite pour être journalisée (`ctx.Log().Warn(errors.X(…))`)
  n'est pas renvoyée et ne compte pas — ce qui corrige aussi des resolvers
  qui journalisent un `Unauthorized` puis ne renvoient rien. Les endpoints
  viennent de l'extracteur Go, que codetospec relie aux règles par leurs
  citations. Une variable de paquet tenant une erreur d'autorisation
  (`var RateLimitExceeded = errors.Unauthorized(…)`) nomme le refus. Une
  fonction qui construit seulement une erreur d'autorisation quelque part
  n'est pas pour autant un contrôle (mesuré : cela lisait des échecs de
  données comme `signup.ProcessSignup` échoue).
- **Authentification des requêtes** : sont aussi des contrôles, sans
  directive ni erreur d'autorisation, (1) les authentificateurs — une
  fonction qui renvoie une erreur, reçoit la requête (`*http.Request`,
  `http.Header`) et lit un secret (`ctx.Secrets().Get`, `secrets.Validate`),
  elle-même ou par une fonction de son paquet : la signature des messages
  pubsub (`pubsub.DecodeAndValidatePushMsg`, échec `InvalidInput`), lue dans
  les paquets de `pkg/` que le service importe (`go.mod` de webapp) ; (2) une
  valeur de la requête (`r.URL.Query().Get`, `r.Header.Get`, `r.FormValue`)
  comparée à une chaîne de paquet dont le nom dit jeton ou secret
  (`_pubsubToken`, heuristique sur le nom, rendue telle quelle) ; (3) une
  enveloppe d'enregistrement dont la closure renvoyée a un garde
  (`serve.CreateTaskHandler` refuse ce qui n'est pas une tâche) : « vérifier
  l'accès à /tasks/… avec `serve.CreateTaskHandler` », cité sur
  l'enregistrement ; (4) une fonction non marquée qui refuse elle-même avec
  une erreur d'autorisation sous une condition sur un contrôle connu
  (`_ensureDevAccess`, `checkAIGuidePermissions`) : son erreur refuse.
- **Règles du code partagé** (citées dans `pkg/`, racine `webapp` :
  `codetospec --ref-root webapp=<checkout>`, que passe `scanwebapp.sh`) : les
  paquets de `pkg/` que le service importe sont lus transitivement, et seules
  les fonctions qu'il **atteint** — appelées ou passées en valeur, d'un paquet
  à l'autre, méthodes et interfaces non suivies (borne basse) — donnent des
  règles : gardes d'une enveloppe (« rejeter les requêtes passant par
  `ValidateGraphqlSecretMiddleware` »), politiques
  (`validateFastlyAuthnHeader`). Le gateway importe `serve` sans atteindre
  son middleware GraphQL, et n'en hérite pas. Domaine `shared-<paquet>`. Une
  comparaison avec un local tenant une valeur de requête
  (`actual := r.Header.Get(H)`) se lit côte à côte : « `r.Header.Get(H)`
  ne vaut pas `expected` ».
- **Contrats d'en-tête** (`rule`, « Contrat d'en-tête : H ») : attributs
  `header`, `contract` (`sends` | `requires` | `forwards`) et `secret` (le nom
  du secret dans le magasin, `secrets.GSM("backend_to_gateway_secret", 1)`,
  le même d'un service à l'autre). `sends` : `….Header.Set|Add("X-…", v)`
  avec `v` lu d'un secret dans la fonction, ou passé en paramètre par un
  appelant du même paquet qui l'a lu (`gqlclient.NewClient` → `newClient`) ;
  `requires` : `secrets.Validate(ctx, S, <en-tête>)` ou un local d'en-tête
  comparé à un local de secret ; `forwards` : une variable de paquet que
  parcourt une boucle qui copie des en-têtes (`_reqHeaderForwardingMap` du
  gateway), une règle pour la liste.
- **Enregistrements conditionnels** (`rule`, « Enregistrement : / ») : un
  `serve.HandleFunc|Handle(path, …)` sous des `if` (le `else` nié), quel que
  soit le handler : « Le systeme doit servir / seulement si
  `os.Getenv("KA_IS_DEV_SERVER") == "1"` est vrai » (le playground du
  gateway), cité du `if` extérieur à l'enregistrement. Les règles du code
  partagé et ses contrats ont toujours le domaine `shared-<paquet>`.
- **Politiques d'accès** (`rule`, « Politique d'accès : _validatePermission »)
  : une fonction qui ne renvoie qu'une erreur, accepte sur certains chemins
  (`return nil`) et finit par un refus (erreur d'autorisation, ou erreur d'un
  contrôle qui en construit une : `return validateErr`) est une liste
  d'autorisation ; la règle donne chaque condition d'acceptation (branche,
  imbrication, « pour un `x` de `xs` » en boucle), jointes par OU : « Le
  systeme doit accepter une requête vérifiée par `_validatePermission`
  seulement si `isDev` est vrai OU (…) …, et la rejeter sinon avec l'erreur
  `Unauthorized` ». `v, ok := m[k]` se lit « `k` est une clé de `m` ».
- **Tables d'accès** (`rule`) : une table lue par un contrôle a ses propres
  règles, citées sur ses entrées. Une table de limites de débit
  (`rules := _rateLimits[op]` puis `ratelimit.ByIP(…,
  MaxRequestsPerWindow(rules.maxIPCount), LimitWindow(rules.ipWindow))`)
  donne une règle par opération (« limiter `createProgram` à 300 requêtes par
  heure par adresse IP et à 25 requêtes par heure par compte » ; un compteur
  absent ne s'applique pas). Une table lue par une politique, une fonction
  qu'elle appelle ou une fonction de limite donne ses entrées : ensemble
  (`map[string]bool`, `_csrfExempt`), correspondances (`map[string]string`,
  `oldMobileExceptions`), listes par clé (`map[string][]string`,
  `_safelistedKAIDsForOperation`). La table et son lecteur sont nommés sans
  backticks : leurs noms ne sont pas sur les lignes citées.
- **Limites de débit** (`rule`, « Limite de débit : … ») : chaque
  `if ratelimit.ByIP|ByAccount|ByEmailRecipient(ctx, "Clé", …) { return … }`
  (vrai au-delà de la limite), avec la limite écrite comme dans le code pour
  le grounding : « l'appelant dépasse la limite de débit `Clé` par adresse IP
  (200 requêtes par 600 secondes) » ; « sa limite par défaut » sans options.
- **Domaines** (`domain`) : chaque méthode de resolver est rattachée au domaine
  du fichier de schéma qui déclare son champ (`auth_emails.graphql` →
  `auth-emails`), d'après `gqlgen.yml` et `vektah/gqlparser/v2` (le parseur de
  gqlgen). Les schémas partagés hors service (`../../pkg/...`) sont ignorés.
  Le type GraphQL d'un resolver est lu dans les fabriques de `Resolver` :
  une méthode sans paramètre qui renvoie une interface `…Resolver` et
  construit `&impl{…}`. L'interface générée nomme le type
  (`graphql.MutationResolver` → `Mutation`, même pour l'alias
  `InvalidatePracticeTaskByID()` de progress) ; une interface locale
  (`gapFillerContentSequenceDataResolver`), le nom de la méthode. Un type Go
  qui sert plusieurs types GraphQL (l'`entityResolver` de gap-finder) les
  départage par méthode : celui dont le schéma déclare le champ. Une autre
  méthode de `Resolver` (`GetServiceSpecificKAContext(ctx)`) n'est pas une
  fabrique ; sans fabrique, le nom du type Go sert de repli
  (`mutationResolver` → `Mutation`). Un finder de fédération (`entityResolver.FindMany<T>By<Clés>s`,
  `Find<T>By<Clé>`) prend le domaine du fichier qui déclare le type `@key` `T`
  (le plus long qui convient : `RowForActivationReportByTeacherV2`), ou à
  défaut qui l'étend quand le `@key` est dans un schéma partagé. Une méthode
  non exportée d'un resolver (`e._deleteUserAccountBlock`), que gqlgen
  n'appelle jamais, est une auxiliaire : ses règles portent l'opération de
  ses appelants quand ils s'accordent (« Mutation.deleteUserAccountBlock (via
  _deleteUserAccountBlock) »). Seule une méthode exportée sans champ de
  schéma est signalée (code mort : `DevadminTriggerAlarm`).
  Un schéma fourre-tout — plus de la moitié des méthodes, réparties sur au
  moins 10 fichiers (le `schema.graphql` de districts) — ne nomme pas de
  domaine : ses méthodes prennent celui de leur fichier de resolvers
  (`user_district_info.go` → `user-district-info`).
  Un fichier de resolvers dont toutes les méthodes ont le même domaine le
  donne au fichier entier ; une fonction auxiliaire prend en outre le
  domaine de ses appelants quand ils s'accordent (`acls.go`, ou un fichier
  dont les méthodes couvrent plusieurs domaines), de proche en proche.
  `schema.graphql`, le fichier par défaut de gqlgen, ne nomme pas de domaine
  quand le service a d'autres schémas : c'est le fourre-tout.
- **Endpoints** (`route`) : chaque champ racine (`Query`, `Mutation`,
  `Subscription`) qui a une méthode de resolver, et chaque finder de
  fédération (méthode `ENTITY` : la requête `_entities` du gateway l'appelle
  pour tout service qui référence le type), avec `handler_path` ; les règles
  d'autorisation y sont reliées (`exposed_by`).
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
  du dossier en majuscules). Attribut `as` : `service-admin` quand le client
  est construit par `….AsServiceAdmin()` (requête service-admin, que les
  contrôles `RequestIsServiceAdminQuery` acceptent), `user` pour `AsUser()`,
  absent pour un client en variable.

## Usage

```sh
go build -o extract .
./extract -root ~/khan/webapp/services/users -lang en
./extract -all -root ~/khan/webapp -lang en   # tout webapp, consolidé
```

`-all` analyse chaque service de `services/` qui contient du Go, comme un
service seul, puis consolide : les citations deviennent relatives à la racine
du checkout (`services/<svc>/…`, `pkg/…`), les identifiants, domaines
(`<svc>-<domaine>`) et liens de route sont préfixés par le service, chaque fait
porte `in_services`, et une règle du code partagé n'apparaît qu'une fois, avec
la liste des services qui en héritent. `scanwebapp-authz.sh` (à la racine du
dépôt) l'enchaîne avec l'extracteur Go et `scripts/authzall.py` :
`~/authz-webapp/authz.html`, `authz.csv`, `authz.md`, en une minute et demie,
sans LLM.

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

## Vue des autorisations

`uv run scripts/authzview.py` (depuis la racine du dépôt) écrit
`~/spec-graphs/authz.html` : par service, opérations × contrôles, filtres,
vérifications nommées, accès non vérifiés (`acl.TODO`) et trous, à partir des
sorties de `scanwebapp.sh` ; les rôles du service admin (vue « By role » et
« granted by » de chaque permission), et la règle des administrateurs du site
(toutes les capacités, citée depuis `scopes.go`, sous réserve du drapeau
`UserData.HasAnyPermissions` synchronisé). `--fresh` relance
l'extracteur courant (secondes, sans LLM) au lieu des faits du dernier scan.
Page locale, sans réseau.

## Tests

```sh
go test ./...                    # golden testdata/golden.{fr,en}.json
UPDATE_GOLDEN=1 go test ./...    # régénérer, puis relire le diff
```
