# Policy inventory

1 enforcement points, 2 checks, 1 open, 0 todo, 1 unguarded; inputs looked for 3 calls deep.

## Enforcement points

| Endpoint | Guard | Checks called |
|---|---|---|
| endpoint.query-cdi | rule.classroom.authz-query-cdi | acl.IsCurrentUser [self] (kaid); permissions.CanViewCDI [check] |

## Checks

### acl.IsCurrentUser

marked

### permissions.CanViewCDI

marked, permissions/permissions.go:10-20

| Kind | Input | Detail | Declared by |
|---|---|---|---|
| field | models.ClassroomDistrictInfo.AllTeacherUDIsIDs |  | teacher_of, minus grantees |
| field | models.UserDistrictInfo.StudentOfCDIKeys |  |  |
| flag | feature_flags.MoaEnforcementFlag | moa-enforcement |  |
| load | UserDistrictInfo | kaid | hasUDI |
| staff | capabilities.CanActLikeADistrictAdmin | CAN_ACT_LIKE_A_DISTRICT_ADMIN |  |
| value | models.Student | STUDENT |  |

- rule.permissions.view-cdi: Teachers and students view their class (crosscheck: supported)

## Undeclared inputs

- models.UserDistrictInfo.StudentOfCDIKeys

## Registry entries no check reads

- isChild

## Open access

- rule.misc.authz-query-time (resolvers/time.go:3-3)

## Unguarded handlers

- StudentList.legacy (resolvers/x.go:3-9)

## Limits

- Complete relative to the checks the extractor recognizes; a handler with none is listed as unguarded.
- Inputs are looked for 3 calls deep, interface dispatch left out.
- Reads through maps, reflection or encoded blobs are not field reads.
- "Declared" means the same source as a registry entry, not the same meaning.
