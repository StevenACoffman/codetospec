# webapp extractor (Khan Academy)

codetospec extractor for the Go services of `Khan/webapp` (GraphQL via
gqlgen), on the `codetospec/facts/v1` protocol. Without an LLM, it emits what
a model reproduces at high cost and approximately, or not at all:

- **Authorization rules** (`rule`): the access guard of each gqlgen resolver
  method. A check is first what webapp declares as one: any function,
  interface method or variable marked `//ka:permission-check` in the service
  (the directive its linter `dev/linters/permissions_lint.go` requires,
  ADR-211), and those marked by webapp's shared packages (`pkg/khan/acl`,
  `pkg/web`: option `-webapp`, default `<root>/../..`; only the files of
  `pkg/` carrying the directive are read, and the service keeps control of its
  own markings). The error of such a check is a refusal only if the check
  itself builds an authorization error (`requireLoggedInAdminKaid` →
  `errors.Unauthorized`); otherwise it is a read failure (`GetAllVisibleUDIs`
  refuses with an empty list, `UserIsKADSSSuperAdmin` with `false`) and
  `if err != nil` stays plumbing. The form `if err := check(); err != nil {…}`
  is recognized. Added to these are
  (`acl.IsLoggedIn`, `acl.IsCurrentUser`,
  `acl.ActorHasPermission[Err]`, `acl.IsCoachedByActor`,
  `ktx.RequestIs…()`, the predicates of the service's access package —
  exported functions of `permissions/` whose first result is a `bool`, option
  `-access-pkgs`; loaders such as `GetAllVisibleUDIs` are not —
  and the `acl.OpenAccess()` marker), rendered as an EARS requirement
  that **names the GraphQL operation** (`Mutation.addAuthEmail`), cited on
  the exact lines of the guard. The body is walked branch by branch
  (`if`/`else`, `else if`, unlabeled `switch`, `range` loops, up to
  4 levels): a guard's condition is that of the branches leading to it
  AND its own ("`kaid == nil` does not hold AND the caller is not the user
  `kaid`"), the `default` of a `switch` means "none of the cases",
  and an `else if` after a branch that returns reads as the next
  statement. A guard is a block that **ends** with a `return` (or, in a
  loop, with `continue`: "reject that `udiID`"; a `return` inside a loop
  reads "for some `cdi` in `cdis`") and whose own condition uses a check, or
  that returns an authorization error on its own path:
  `errors.Unauthorized`/`NotAllowed`/`Forbidden`/
  `PermissionDenied`, or a GraphQL code `…ErrorCodeUnauthorized`.
  Variables are replaced by what they hold, call or condition
  (`authorized := a || b` then `if !authorized`), including the idiom
  `ok := A; if !ok { ok = B }` ("A OR B"); a variable reassigned in a
  nested block is no longer attached. The error of a call reads "`f(…)`
  fails", and makes a guard only if the call refuses through its error — itself
  or, by delegation, a function it calls (`updateUserRoleValidation`
  → `getValidatedAdminKaid`). `acl.IsCurrentUser` on the caller's own kaid
  (`kaid := user.Kaid`, `user` from `RequestUser()`) is flagged
  "(never true)": this check refuses no one. The result is named:
  rejection with the `X` error, or "return no data" when the guard
  returns zero or empty values (`return nil, nil`, `[]T{}`). An
  unknown predicate is quoted as is (never paraphrased).
- **Visibility** (`rule`, "Visibility: …"): each call of a marked
  filter — first result a collection, an error that does not refuse
  (`permissions.GetAllVisibleUDIs`) — limits the operation to what the filter
  lets the caller see.
- **Unchecked access** (`rule`, "Unchecked access: …"): `acl.TODO()`,
  which webapp documents as an empty check marking a resolver without a
  permission check ("a potential security risk").
- **Roles** (`rule`, "Role: …", admin service): each
  `RegisterRoleDefinition(RoleDefinition{…})` grants its capabilities —
  "The system shall grant the `supporter` role (…) the capabilities
  `CanViewUserIdentity`, …"; attributes `role` and `capabilities` for the
  join with the permissions the services check.
- **Access check** (`rule`, "Access check: …"): a resolver with no other
  rule names the first check it references — webapp's linter requires
  one — without claiming to say how it decides. Failing that, an alias names
  the exported method of the same resolver that it calls
  (`FindManyStudentListByDescriptors` → `r.FindManyClassroomByDescriptors`),
  including on a rebuilt receiver
  (`(&mutationResolver{r.Resolver}).SyncActiveAssignmentsInClassroomToLms`),
  only if that method has a rule.
  The forms that need data flow stay at this level: a deferred
  error (`err = errors.Unauthorized()` returned later), chained errors
  (`if err == nil { err = check() }`), a granting branch
  (`if canView { result = … }`).
- **net/http handlers** (`rule`, "Access control: /safelist/add"):
  each handler registered by `serve.HandleFunc(path, h)` /
  `serve.Handle(path, h)` outside `resolvers/` — method value
  (`rts.HandleGraphQL`), function, closure that only calls one, or
  wrapper (`serve.CreateTaskHandler(f)`) — is walked like a resolver;
  the rule names its paths. This is the case of the graphql-gateway, which has
  neither `gqlgen.yml` nor resolvers. A refusal also reads from the status
  written (`w.WriteHeader(http.StatusUnauthorized)`, `http.Error(…, 403)`); an
  error built to be logged (`ctx.Log().Warn(errors.X(…))`)
  is not returned and does not count — which also corrects resolvers
  that log an `Unauthorized` and then return nothing. The endpoints
  come from the Go extractor, which codetospec links to rules by their
  citations. A package variable holding an authorization error
  (`var RateLimitExceeded = errors.Unauthorized(…)`) names the refusal. A
  function that merely builds an authorization error somewhere
  is not thereby a check (measured: this read data failures
  such as `signup.ProcessSignup` fails).
- **Request authentication**: also checks, with no
  directive or authorization error, are (1) authenticators — a
  function that returns an error, receives the request (`*http.Request`,
  `http.Header`) and reads a secret (`ctx.Secrets().Get`, `secrets.Validate`),
  itself or through a function of its package: the signature of pubsub
  messages (`pubsub.DecodeAndValidatePushMsg`, failure `InvalidInput`), read in
  the `pkg/` packages the service imports (webapp's `go.mod`); (2) a
  request value (`r.URL.Query().Get`, `r.Header.Get`, `r.FormValue`)
  compared with a package string whose name says token or secret
  (`_pubsubToken`, a heuristic on the name, rendered as is); (3) a
  registration wrapper whose returned closure has a guard
  (`serve.CreateTaskHandler` refuses what is not a task): "check
  access to /tasks/… with `serve.CreateTaskHandler`", cited on
  the registration; (4) an unmarked function that refuses on its own with
  an authorization error under a condition on a known check
  (`_ensureDevAccess`, `checkAIGuidePermissions`): its error refuses.
- **Shared code rules** (cited in `pkg/`, root `pkg`:
  `codetospec --ref-root pkg=<checkout>/pkg`, which `scanwebapp.sh` passes): the
  `pkg/` packages the service imports are read transitively, and only
  the functions it **reaches** — called or passed as values, from one package
  to another, methods and interfaces not followed (lower bound) — give
  rules: guards of a wrapper ("reject requests through
  `ValidateGraphqlSecretMiddleware`"), policies
  (`validateFastlyAuthnHeader`). The gateway imports `serve` without reaching
  its GraphQL middleware, and does not inherit it. Domain `shared-<package>`. A
  comparison with a local holding a request value
  (`actual := r.Header.Get(H)`) reads side by side: "`r.Header.Get(H)`
  is not `expected`".
- **Reached shared code** (`reach`, root `pkg`): each declaration of
  `pkg/` that the service reaches (the same reading as above: functions,
  variables, types, and the methods of a reached type). codetospec derives
  from it the rules inherited from the `pkg/` spec (`codetospec run --inherit
  pkg=<graph>`, see `internal/inherit`); `scanwebapp.sh` passes it as soon
  as `~/spec-graphs/pkg` exists (`PKG_GRAPH`).
- **Header contracts** (`rule`, "Header contract: H"): attributes
  `header`, `contract` (`sends` | `requires` | `forwards`) and `secret` (the
  name of the secret in the store, `secrets.GSM("backend_to_gateway_secret", 1)`,
  the same from one service to the next). `sends`: `….Header.Set|Add("X-…", v)`
  with `v` read from a secret in the function, or passed as a parameter by a
  caller in the same package that read it (`gqlclient.NewClient` → `newClient`);
  `requires`: `secrets.Validate(ctx, S, <header>)` or a header local
  compared with a secret local; `forwards`: a package variable that a
  header-copying loop walks (the gateway's `_reqHeaderForwardingMap`), one
  rule for the list.
- **Conditional registrations** (`rule`, "Registration: /"): a
  `serve.HandleFunc|Handle(path, …)` under `if`s (the `else` negated), whatever
  the handler: "The system shall serve / only when
  `os.Getenv("KA_IS_DEV_SERVER") == "1"` holds" (the gateway's
  playground), cited from the outer `if` to the registration. Shared-code
  rules and their contracts always have the domain `shared-<package>`.
- **Access policies** (`rule`, "Access policy: _validatePermission")
  : a function that returns only an error, accepts on some paths
  (`return nil`) and ends with a refusal (an authorization error, or the error of a
  check that builds one: `return validateErr`) is an
  allowlist; the rule gives each acceptance condition (branch,
  nesting, "for some `x` in `xs`" in a loop), joined by OR: "The
  system shall accept a request checked by `_validatePermission`
  only when `isDev` holds OR (…) …, and reject it otherwise with the
  `Unauthorized` error". `v, ok := m[k]` reads "`k` is a key of `m`".
- **Access tables** (`rule`): a table read by a check has its own
  rules, cited on its entries. A rate-limit table
  (`rules := _rateLimits[op]` then `ratelimit.ByIP(…,
  MaxRequestsPerWindow(rules.maxIPCount), LimitWindow(rules.ipWindow))`)
  gives one rule per operation ("limit `createProgram` to 300 requests per
  hour by IP and to 25 requests per 2 hours by account"; a counter
  left out does not apply). A table read by a policy, a function
  it calls or a limit function gives its entries: set
  (`map[string]bool`, `_csrfExempt`), mappings (`map[string]string`,
  `oldMobileExceptions`), lists by key (`map[string][]string`,
  `_safelistedKAIDsForOperation`). The table and its reader are named without
  backticks: their names are not on the cited lines.
- **Rate limits** (`rule`, "Rate limit: …"): each
  `if ratelimit.ByIP|ByAccount|ByEmailRecipient(ctx, "Key", …) { return … }`
  (true past the limit), with the limit written as in the code for
  grounding: "the caller exceeds the `Key` rate limit by IP
  (200 requests per 600 seconds)"; "its default limit" without options.
- **Domains** (`domain`): each resolver method is attached to the domain
  of the schema file that declares its field (`auth_emails.graphql` →
  `auth-emails`), from `gqlgen.yml` and `vektah/gqlparser/v2` (gqlgen's
  parser). Shared schemas outside the service (`../../pkg/...`) are ignored.
  A resolver's GraphQL type is read from the `Resolver` factories:
  a method with no parameter that returns a `…Resolver` interface and
  builds `&impl{…}`. The generated interface names the type
  (`graphql.MutationResolver` → `Mutation`, even for progress's alias
  `InvalidatePracticeTaskByID()`); a local interface
  (`gapFillerContentSequenceDataResolver`), the method's name. A Go type
  that serves several GraphQL types (gap-finder's `entityResolver`) tells them
  apart by method: the one whose schema declares the field. Another
  `Resolver` method (`GetServiceSpecificKAContext(ctx)`) is not a
  factory; with no factory, the Go type's name is the fallback
  (`mutationResolver` → `Mutation`). A federation finder (`entityResolver.FindMany<T>By<Keys>s`,
  `Find<T>By<Key>`) takes the domain of the file that declares the `@key` type `T`
  (the longest that fits: `RowForActivationReportByTeacherV2`), or
  failing that the file that extends it when the `@key` is in a shared schema. An
  unexported resolver method (`e._deleteUserAccountBlock`), which gqlgen
  never calls, is a helper: its rules carry the operation of
  its callers when they agree ("Mutation.deleteUserAccountBlock (via
  _deleteUserAccountBlock)"). Only an exported method with no schema
  field is flagged (dead code: `DevadminTriggerAlarm`).
  A catch-all schema — more than half the methods, spread over at
  least 10 files (districts' `schema.graphql`) — names no
  domain: its methods take that of their resolvers file
  (`user_district_info.go` → `user-district-info`).
  A resolvers file whose methods all have the same domain
  gives it to the whole file. A helper function takes the domain of its
  callers when they agree: the core does this, from the
  `call` facts of the Go extractor (which `scanwebapp.sh` runs on each
  service).
  `schema.graphql`, gqlgen's default file, names no domain
  when the service has other schemas: it is the catch-all.
- **Endpoints** (`route`): each root field (`Query`, `Mutation`,
  `Subscription`) that has a resolver method, and each federation
  finder (method `ENTITY`: the gateway's `_entities` query calls it
  for any service that references the type), with `handler_path`; the
  authorization rules are linked to them (`exposed_by`).
- **Entities** (`table`): each `datastore.RegisterKind("Kind", new(T))`,
  columns = fields of `T` as Datastore stores them (`datastore` tag),
  `description` = the doc comment of `T`, on one line.
- **Docs** (`doc`): GraphQL description of the field and its arguments,
  shown to the map for the method that resolves it.
- **Cross-service calls** (`dependency`): each
  `genqlient.<Op>(…)` call one of whose root fields belongs to another subgraph
  (`@join__field` of the composed schema `gengraphql/composed_schema.graphql`).
  Options `-supergraph` (folder of the composed schema, default
  `<root>/../../gengraphql`) and `-graph` (the service's subgraph, default: the
  folder name in capitals). Attribute `as`: `service-admin` when the client
  is built by `….AsServiceAdmin()` (a service-admin query, which
  `RequestIsServiceAdminQuery` checks accept), `user` for `AsUser()`,
  absent for a client held in a variable.

## Usage

```sh
go build -o extract .
./extract -root ~/khan/webapp/services/users -lang en
./extract -all -root ~/khan/webapp -lang en   # all of webapp, consolidated
```

`-all` analyzes each service of `services/` that contains Go, as a
single service, then consolidates: citations become relative to the root
of the checkout (`services/<svc>/…`, `pkg/…`), ids, domains
(`<svc>-<domain>`) and route links are prefixed by the service, each fact
carries `in_services`, and a shared-code rule appears only once, with
the list of services that inherit it. `scanwebapp-authz.sh` (at the root of the
repository) chains it with the Go extractor and `scripts/authzall.py`:
`~/authz-webapp/authz.html`, `authz.csv`, `authz.md`, in a minute and a half,
without an LLM.

```yaml
# codetospec.yaml
extractors:
  - name: webapp
    cmd: /path/to/extractors/webapp/extract
    args: [-root, "{src}", -lang, "{lang}"]
```

codetospec's `--src` must be the service folder (the one that contains
`gqlgen.yml` and `resolvers/`). Warnings (unparsable file,
method with no schema field) go to stderr.

## Authorization view

`uv run scripts/authzview.py` (from the repository root) writes
`~/spec-graphs/authz.html`: per service, operations × checks, filters,
named checks, unchecked accesses (`acl.TODO`) and gaps, from the
outputs of `scanwebapp.sh`; the admin service's roles ("By role" view and
"granted by" for each permission), and the site administrators' rule
(all capabilities, cited from `scopes.go`, subject to the synced
`UserData.HasAnyPermissions` flag). `--fresh` reruns
the current extractor (seconds, without an LLM) instead of the facts of the last scan.
Local page, no network.

## Tests

```sh
go test ./...                    # golden testdata/golden.{fr,en}.json
UPDATE_GOLDEN=1 go test ./...    # regenerate, then review the diff
```
