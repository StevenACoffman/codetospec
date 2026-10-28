# Verification records

A record keeps a measurement made on real code, so the number can be
checked again later instead of trusted from a commit message. `make
corpus` guards counts that must not get worse; a record holds the
one-off measurements behind a design decision.

Each record, named `<topic>-<YYYY-MM-DD>.md`, states:

- **Question**: what the measurement decides.
- **Revisions**: the codetospec commit (and whether the tree was dirty),
  the commit of each measured source repository, the Go version.
- **Commands**: enough to run it again, including any throwaway program
  (inline, since it is not committed).
- **Results**: the numbers, as measured.
- **Limits**: what the numbers do not show.

Records are never edited after the fact; a new measurement is a new
record.
