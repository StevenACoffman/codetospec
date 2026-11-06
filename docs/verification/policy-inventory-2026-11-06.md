# Policy inventory on districts

Question: does `codetospec policies` find what the manual pass of
2026-11-06 found in districts' `permissions.go`
(`policy_inventory_plan.md` finding 4), and what does a first registry
from the PIP data model leave undeclared?

## Revisions and commands

- codetospec working tree on `ccc0566` with `policy_inventory_plan.md`
  Part 1 and Part 2 (FIX.md R1-R3 included); webapp `c55b04b0090`.
- A scratch copy of `~/spec-graphs/districts`, rerun warm with
  `scanwebapp.sh districts --quality --crosscheck --write-domain-model`
  (0 LLM tokens).
- A first registry written from ident-know `webapp/datamodel.md`'s
  Relations table and resource properties: `teacher_of`, `student_of`,
  `co_teacher_of`, `admin_of` (UDI admin fields, `DistrictKey`, UMI
  `AdminOf`/`AdminOfHeight`), `in`, `isTest`, `hasUMI` (`load
  UserMetaInfo`), `hasDistrictOrSchoolAdminUDI` (`PrimaryRole`),
  `currentSchoolYearEnd`.
- `bin/codetospec policies --out <copy> --registry <file> [--format json]`.

## Results

- 222 enforcement points (endpoints and field resolvers' operations),
  39 checks (all with a citation), 148 open, 0 todo, 0 unguarded.
- Recall against the manual pass (finding 4), all six found and placed:

  | input | checks reading it (first three) |
  |---|---|
  | `ClassroomDistrictInfo.AllTeacherUDIsIDs` | `grantedClassroomFeatures`, `CanManageDistrictClassroom`, `CanManageDistrictStudent` |
  | `UserDistrictInfo.StudentOfCDIKeys` | `CanManageDistrictClassroom`, `CanManageDistrictStudent`, `CanViewCDI` (through `GetStudentOfCDIsIDs()`) |
  | `UserDistrictInfo.PrimarySchoolID` | `CanManageDistrictTeacher` |
  | `UserMetaInfo.CanManageAdmins` | `ActorCanClearUMI`, `ActorCanSetUpAndEditUMI` |
  | `District.OfferingValues` | `grantedClassroomFeatures`, `CanManageDistrictClassroom`, `CanManageDistrictStudent` |
  | the `moa-enforcement` flag | `governingMoa`, `UserAdministersAdminAggregate` |

- Staff inputs: 7 capabilities, among them `CanActLikeADistrictAdmin`
  and `CanAccessDevadminSupport`, each with its stored value.
- 28 of 39 checks have inputs; the rest read nothing the module declares
  (they delegate to shared or cross-service code).
- Registry: 83 field and load inputs undeclared; 4 entries no check reads
  (`co_teacher_of`, `currentSchoolYearEnd`, `hasUMI`, `isTest`).
  `co_teacher_of` agrees with the data model's finding that no access
  check reads the grant (its access goes through the CDI's
  `teacher_udis`); `hasUMI` isn't loaded through `datastore.NewQuery` in
  these checks.
- Precision, read for `CanManageDistrictTeacher`
  (`permissions.go:501-575`): its fields, loads, the KMAP offering
  constant and the capability bypass all match the code. Noise: the
  closure reaches the dataloader package three calls down and brings its
  constants (`loaders.InLimit`, `loaders.loadersKey`) as values.

## Limits

- The ground truth is finding 4's list, not a per-function list for all
  39 checks: recall is of those six, precision of one check read.
- A datastore kind held in a variable rather than a constant appears as
  its source (`models.ClassroomDistrictInfoKind`).
- Registry entries name fields and loads; methods aren't supported yet.
- The 11-item undeclared list of the manual comparison isn't in the
  repository, so the 83 aren't compared with it.
