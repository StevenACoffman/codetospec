# Policy inventory

0 enforcement points, 10 checks, 0 open, 0 todo, 0 unguarded; inputs looked for 3 calls deep.

## Enforcement points

| Endpoint | Guard | Checks called |
|---|---|---|

## Checks

### permissions.AdminOrSelf

marked, permissions/permissions.go:10-15

> Allowed when the request is an administrator call from another service, or the caller is the user concerned.

Definition: pinned; stale: the code changed since the pin.

Allowing paths:

1. `{"var":"request.isServiceAdmin","op":"equal","value":true}` (permissions/permissions.go:11-11)
2. `{"all":[{"not":{"var":"request.isServiceAdmin","op":"equal","value":true}},{"var":"acl.IsCurrentUser","op":"equal","value":true}]}` (permissions/permissions.go:14-14)

Leaves: service_admin (grounded), self (grounded).

Pin alternatives: service_admin; self.
Coverage: every path matches an alternative and every alternative a path.

Second reader: equivalent.

### permissions.Cross

marked, permissions/permissions.go:60-65

> Not stated: cross_service.

Definition: unpinnable (cross_service).

Second reader: no pin in the second file.

### permissions.Delegating

marked, permissions/permissions.go:30-35

> Allowed when the request is an administrator call from another service, or the caller is the user concerned, or the caller has the relation "ghost".

Definition: pinned.

Allowing paths:

1. `{"var":"permissions.AdminOrSelf.fails","op":"equal","value":false}` (permissions/permissions.go:13-13)

Leaves: service_admin (unknown), self (unknown), relation ghost (unknown).

Pin alternatives: service_admin; self; data.
Coverage: alternative 3 matches no path.

Not in the registry: ghost.

Second reader: no pin in the second file.

### permissions.Either

marked, permissions/permissions.go:40-45

> Allowed when the caller is signed in, or the request is an administrator call from another service. (Approximate: the code also allows some requests this statement leaves out.)

Definition: pinned; approximate: stricter than the code.

Allowing paths:

1. `{"any":[{"var":"caller.loggedIn","op":"equal","value":true},{"var":"request.isServiceAdmin","op":"equal","value":true}]}` (permissions/permissions.go:12-12)

Leaves: login (grounded), service_admin (grounded).

Pin alternatives: login; service_admin.
Coverage: every path matches an alternative and every alternative a path.

Second reader: no pin in the second file.

### permissions.Looping

marked, permissions/permissions.go:50-55

> Allowed when the caller is signed in.

Definition: pinned.

Allowing paths: not analyzable (a loop, switch or select decides).

Leaves: login (unknown).

Second reader: no pin in the second file.

Question q-12e57db88788 (dead_branch): the loop's second return never runs

### permissions.Managed

marked, permissions/permissions.go:70-75

> Allowed when the caller holds the capability "View" for the user concerned, or the caller has the relation "manages".

Definition: pinned.

Allowing paths:

1. `{"var":"caller.hasPermission.CanView","op":"equal","value":true}` (permissions/permissions.go:71-71)
2. `{"all":[{"not":{"var":"acl.ActorHasPermissionErr.fails","op":"equal","value":true}},{"not":{"var":"caller.hasPermission.CanView","op":"equal","value":true}},{"var":"isManagedByActor","op":"equal","value":true}]}` (permissions/permissions.go:73-73)

Leaves: capability CanView (grounded), relation manages (unknown).

Pin alternatives: capability CanView; data.
Coverage: every path matches an alternative and every alternative a path.

Not in the registry: manages.

Second reader: no pin in the second file.

### permissions.Outer

marked, permissions/permissions.go:90-95

> Allowed when the request is an administrator call from another service, or the caller is the user concerned.

Definition: pinned.

Allowing paths:

1. `{"var":"AdminOrSelf.fails","op":"equal","value":false}` (permissions/permissions.go:94-94)

Leaves: service_admin (unknown), self (unknown).

Pin alternatives: service_admin; self.
Coverage: every path matches an alternative and every alternative a path.

Second reader: no pin in the second file.

### permissions.Teacher

marked, permissions/permissions.go:20-25

| Kind | Input | Detail | Declared by |
|---|---|---|---|
| field | models.UserDistrictInfo.StudentOfCDIKeys |  | teacher_of |

> Allowed when the caller is signed in, the caller teaches the student and the caller holds the capability "View" everywhere.

Definition: pinned.

Allowing paths:

1. `{"all":[{"var":"caller.loggedIn","op":"equal","value":true},{"var":"classroom.isTeacher","op":"equal","value":true}]}` (permissions/permissions.go:12-12)

Leaves: login (grounded), relation teacher_of (grounded), capability CanView (ungrounded).

Pin alternatives: login ∧ capability CanView ∧ data.
Coverage: path 1 matches no alternative.
Coverage: alternative 1 matches no path.

Second reader: different: only the second pin allows when `{"means":"capability","scope":"global","capability":"CanView"}` false, `{"means":"login"}` true, `{"means":"relation","relation":"teacher_of"}` false.

### permissions.UsesFilterBool

marked, permissions/permissions.go:100-105

> Allowed when the caller has the relation "district_admin_of".

Definition: pinned.

Allowing paths:

1. `{"var":"Visible","op":"equal","value":true}` (permissions/permissions.go:104-104)

Leaves: relation district_admin_of (unknown).

Pin alternatives: data.
Coverage: every path matches an alternative and every alternative a path.

Not in the registry: district_admin_of.

Second reader: no pin in the second file.

### permissions.Wider

marked, permissions/permissions.go:80-85

> Allowed when the caller is signed in.

Definition: pinned.

Allowing paths:

1. `{"any":[{"var":"caller.loggedIn","op":"equal","value":true},{"var":"request.isServiceAdmin","op":"equal","value":true}]}` (permissions/permissions.go:12-12)

Leaves: login (grounded).

Pin alternatives: login.
Coverage: path 1 matches no alternative.

Second reader: no pin in the second file.

## Limits

- Complete relative to the checks the extractor recognizes; a handler with none is listed as unguarded.
- Inputs are looked for 3 calls deep, interface dispatch left out.
- Reads through maps, reflection or encoded blobs are not field reads.
- "Declared" means the same source as a registry entry, not the same meaning.
