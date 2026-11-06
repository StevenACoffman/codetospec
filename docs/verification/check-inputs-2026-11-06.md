# What a function reads: reads, uses_value, loads, struct tags

Question: once the Go extractor states what each function reads
(`policy_inventory_plan.md` Part 2, design 5-7), how large is it, how
long does it take, and does it find what the manual pass found?

## Revisions and commands

- codetospec working tree on `ccc0566` (`extractors/go/inputs.go`,
  `structs.go` tags), on top of the calls-through-variables change;
  webapp `c55b04b0090`.
- `go-extract -root . -skip-dirs testutil` in `services/districts`, with
  the binary before and after.

## Results

- Facts file 19.8 MB → 21.7 MB (+9.5 %); extraction 3.6 s → 3.7 s.
- New facts: 4,792 `reads`, 1,584 `uses_value`, 170 `loads` (146 with
  the properties they filter by); 62 struct facts carry `tags`.
- Value types used most: `string` 172, `models.Role` 124,
  `models.RosterSource` 108, `permissions.CapabilityName` 105 (each with
  its stored value, such as `CAN_ACT_LIKE_A_DISTRICT_ADMIN`, read from
  the generated variable), `models.AuditLogSubKind` 90.
- `CanViewCDI` (`permissions/permissions.go:723`) reads
  `models.ClassroomDistrictInfo.AllTeacherUDIsIDs` and
  `models.UserDistrictInfo.PrimaryRole` and uses `models.Student`. Its
  student check reads `StudentOfCDIKeys` inside `GetStudentOfCDIsIDs()`:
  a read of the method, which the inventory reaches through call facts
  (design 10), not of `CanViewCDI` itself.

## Limits

- Values are those the module's own packages declare: a shared `pkg/`
  constant used by a service is left out (the packages load without
  module information to tell webapp's shared code from a dependency).
- `loads` reads `NewQuery`, `FilterField` and `Filter` of a package named
  `datastore`; a property set through another call (a dataloader's key
  argument) is missed.
- One function's queries share their properties: a function running two
  queries reports both kinds with the union.
