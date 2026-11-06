# NLP4RE tools worth examining for codetospec

Reviewed 2026-11-06. Sources:

- `~/Documents/agent-orange/write/identify_difference_between_business_domain_concepts_and_nfr.md`
  (the "domain concept vs. NFR" write-up and its tool list).
- The NLP4RE tools catalog, `~/Documents/agent-green/nlp4re-tools`
  ([JulianFrattini/nlp4re-tools](https://github.com/JulianFrattini/nlp4re-tools),
  Frattini et al. 2025): 126 tools from 2019–2023, 65 of them with a
  source or artifact link.

Every linked repository and archive named below was opened on that date
(GitHub API, Zenodo API) to check what it contains, its license and its
last push. Maintenance status is recorded, but it is not held against a
tool: most are one-paper research prototypes, and that is fine when what
codetospec takes is data, a method or a test oracle.

## How these tools relate to codetospec

The tools read **prose requirements**. codetospec reads **code** and
writes prose requirements (EARS rules), so it sits at the other end of
their pipelines. That leaves three ways a tool can matter:

1. **Labeled data or ground truth** to measure a codetospec phase
   against (in the `docs/verification/` sense): `--quality`, the
   business/technical nature, citations.
2. **An independent reader of codetospec's output**: an EARS sentence is
   a requirement, so a tool that parses requirements can check what the
   map and the reduce wrote, the way `grounding` and the crosscheck do.
3. **A method to reimplement in Go**, without the tool itself (the main
   module's dependency set is locked, and almost all of these are Python
   notebooks).

None of them is a candidate dependency. Licenses matter only for what
codetospec would copy: code under GPL-3.0, or code with no license at
all, can be read for ideas, but nothing can be vendored from it. GPL-3.0
data used for a measurement should be downloaded at evaluation time
rather than committed to `testdata/`.

## Worth a closer look

### 1. Labeled functional/quality datasets: measure `--quality` and nature

- [explainable-re/RE-2019-Materials](https://github.com/explainable-re/RE-2019-Materials)
  (Dalpiaz et al., RE 2019; GPL-3.0; Zenodo
  [3309669](https://doi.org/10.5281/zenodo.3309669)). `Manually tagged
  datasets/` holds nine sets in which each requirement is labeled for
  functional and quality aspects **separately**: `promise-reclass`,
  `dronology`, `reqview`, `leeds`, `wasp`, `esa-eucl-est`, `ds2`, `ds3`,
  plus the combined sets.
- [tobhey/NoRBERT](https://github.com/tobhey/NoRBERT) (Hey et al.,
  RE 2020; GPL-3.0; pretrained models on Zenodo
  [5541679](https://zenodo.org/records/5541679)). Its notebooks classify
  PROMISE NFR at three levels: functional vs. non-functional, the
  frequent NFR classes, and all NFR classes. It also adds
  `Dataset/func_concerns.csv`, which splits functional requirements into
  function, data and behavior, with both annotators' labels and
  Krippendorff's alpha. Use the corrected fold results: the README
  documents a fold-indexing bug fixed after publication.

**Relevance.** `--quality` classifies a rule from its title and
requirement text alone, so PROMISE sentences can go straight through
its prompt. The PROMISE NFR classes map almost one to one onto the
attribute list in `internal/quality`:

| PROMISE | codetospec attribute |
| --- | --- |
| A | availability |
| FT | reliability |
| L | compliance |
| LF | usability |
| MN | maintainability |
| PE | performance_efficiency |
| PO | portability |
| SC | scalability |
| SE | security |
| US | usability |
| O | no clean match |

Operational (O) is the one class that needs a decision before scoring.
Dalpiaz's separate functional and quality labels test the other open
question: whether a rule that is both (the doc's "requirements mix
both", Eckhardt et al. 2016) ends up `technical`, `business` or `open`.
This is the cheapest outside measurement available: the eval pattern of
`internal/crosscheck` (`CODETOSPEC_EVAL`) already exists, and none of
this data comes from code, so it measures the classifier, not the map.

### 2. CiRA: an independent check of condition structure

- [JulianFrattini/cira](https://github.com/JulianFrattini/cira)
  (Apache-2.0, Python, tests in CI, last push 2024-01; demo release on
  Zenodo [8033347](https://zenodo.org/records/8033347)). For each
  sentence it classifies whether the sentence is causal, labels causes,
  effects, variables, conjunctions and negations, builds a cause-effect
  graph, and derives a minimal test suite from that graph.
- Predecessor: CEREC (syntactic, no learned model), Zenodo
  [4009160](https://zenodo.org/records/4009160), CC-BY-4.0.

**Relevance.** codetospec's `condition_disagreement`
(`internal/grounding/condition.go`, `disagreement`) compares
**literal values** both ways between a rule's structured condition and
its EARS clauses. It never compares their **logic**: which values are
joined by AND, which by OR, and what sits under a NOT. CiRA reads that
logic out of the sentence. Run it over the EARS text and compare its
graph with `condition.Parse` of the rule's condition, and you have an
LLM-free check that the prose and the condition say the same thing. Its
generated test suites are also a second oracle for the `--acceptance`
examples, which `condition.Evaluate` currently checks only against the
condition the model wrote.

**Limits.** The models were trained on English requirements, so a
`--lang fr` run would need its own measurement. CiRA runs as a separate
Python service (it ships a Dockerfile and `app.py`), so it belongs in an
evaluation script, not the binary.

### 3. TAROT: ground-truth requirement→code links to measure citations

- [huiAlex/TAROT](https://github.com/huiAlex/TAROT) (ASE 2022, Java, no
  license file). Information-retrieval traceability using "consensual
  biterms": word pairs found in both a requirement and the identifiers
  of the code that implements it. `dataset/` ships nine projects with
  gold trace links: `derby`, `drools`, `gantt`, `groovy`, `infinispan`,
  `itrust`, `maven`, `pig`, `seam`.
- Similar: [Shniya3/TLR-KRL](https://github.com/Shniya3/TLR-KRL), on
  the eTour and EANCI datasets. Their use cases are in Italian, and the
  repository translates class names.

**Relevance.** This reverses codetospec's direction: these tools are
given the requirement and look for its code, while codetospec cites the
code a rule came from. The gold links are therefore a benchmark for
something codetospec has never measured against an outside source:
whether a rule describing a known requirement cites the code that
really implements it. The biterm idea also suggests an LLM-free signal,
lexical agreement between a rule's text and the identifiers in its
cited span, as a weak warning beside `grounding`.

**Limits.** All nine projects are Java, and `internal/sitter` has
grammars for Go, JavaScript, PHP, Rust, TypeScript and TSX only. Adding
Java is a grammar plus a query file (AGENTS.md), but that work comes
first. The gold links are at file or class level and come from issues
and use cases, so a comparison has to be made at file level. No license
means the data can be used for measurement but not redistributed.

### 4. Measuring termhood: telling domain concepts from generic code

- [kevinlu1248/pyate](https://github.com/kevinlu1248/pyate) (MIT,
  archived 2023): C-value, Combo Basic, Weirdness and related measures
  on top of spaCy.
- [termsuite/termsuite-core](https://github.com/termsuite/termsuite-core)
  (Apache-2.0, Java, last push 2017): multi-word terms, term variants,
  and specificity against a reference frequency list.
- Automatic terminology extraction and ranking for feature modeling
  (Zhang et al., RE 2022; catalog R096;
  [figshare artifact](https://figshare.com/s/9002ed3093971325a438),
  CC-BY-4.0): ranks domain terms by abstraction level.
- Key Concepts Extraction Tool (Spijkman et al. 2021; catalog R012;
  [bowis.github.io/keyextractor](https://bowis.github.io/keyextractor/),
  no license): splits terms into those a given ontology already knows
  and those it doesn't.

**Relevance.** These are methods to reimplement, not dependencies.
`nature_by_evidence` deliberately reads no words of the rule's text
(`internal/position`), so termhood does not belong there. It does fit
`domainmodel.ConceptsFile` (`internal/domainmodel/enhance.go`), which
proposes concepts for the tables and owned `api_type`s no model names.
Scoring identifiers by how specific they are to this repository against
a cross-repository reference (for example, every graph under
`~/spec-graphs`) would separate domain entities from infrastructure.
That speaks to the write-up's "fuzzy boundary" section, where
*availability* is a quality attribute in one system and a domain
concept in hotel booking. The keyextractor's known/unknown split is the
same move as `domainmodel` matching code entities to a person's model.
[PURE](https://zenodo.org/records/1414117) (79 public requirements
documents, CC-BY-4.0) is a ready-made reference corpus of requirements
vocabulary across domains.

### 5. Terminological ambiguity: one concept under several names

- [RELabUU/revv-light](https://github.com/RELabUU/revv-light) (Dalpiaz
  et al., "Detecting terminological ambiguity in user stories"; PHP, no
  license). It reads the concepts and relationships that
  [visual-narrator-adapted-for-revv](https://github.com/RELabUU/visual-narrator-adapted-for-revv)
  extracts, and flags near-synonym terms across a set of stories.
- [MarcelRobeer/VisualNarrator](https://github.com/MarcelRobeer/VisualNarrator)
  (Python/spaCy, no license file): user stories → entities and
  relationships, written as an OWL ontology.

**Relevance.** codetospec matches entities by normalized name, a regular
plural, or `domain_model_entities`, and it lists
`graph.SharedEntities`. Neither catches two domains that name one
concept differently ("learner" and "student", "course" and "class").
REVV's near-synonym detection over the rules' `touches` and condition
variables would surface those pairs as questions. Visual Narrator itself
matters less, because codetospec gets entities more reliably from
schema, struct and `api_type` facts than from parsing prose.

### 6. EARS/Rupp template converter: test inputs and a vagueness list

- [parv97/EARS-Rupp-s-template-conversion-](https://github.com/parv97/EARS-Rupp-s-template-conversion-)
  (NL2RT, Tiwari et al., RE 2022; catalog R103; notebook; no license).
  It converts free text into EARS or Rupp templates. It ships `Vagueness_Dictionary`, a
  `Basic_english_words` list, and 16 problem descriptions (airline,
  banking, elevator, library and others) under `Problem Specifications/`.

**Relevance.** This is the only tool in the catalog that targets EARS.
The code cannot be reused (no license), but the problem specifications
and their converted output are extra cases for `ears.Parse` and
`ears.Normalized`, in the spirit of `earslint_test.go`. A vague word in
a generated rule ("appropriate", "quickly") is a defect `ears.Check`
does not look for today. Measure its precision first: AGENTS.md records
about 25 % precision for the last clause-level check.

### 7. Coordination ambiguity in EARS sentences

- [SNTSVV/MAANA](https://github.com/SNTSVV/MAANA) (Ezzini et al.,
  ICSE 2021; catalog R129; MIT; Maven source in `maana-source.zip`,
  plus data and ground truth). It detects coordination ambiguity ("A and B or C") and
  prepositional-phrase attachment ambiguity, using domain-specific
  corpora.

**Relevance.** `ears.Parse` already has a known undecidable case: in
"QUAND t AND p", a lowercase "and" stays inside the trigger. MAANA's
heuristics for coordination ambiguity are the published method for
exactly that case. They could decide whether a multi-part trigger or
precondition should be a structured `all`/`any` condition rather than
prose.

### 8. Zero-shot classification as a comparison point

- [waadalhoshan/ZSL4REQ](https://github.com/waadalhoshan/ZSL4REQ)
  (catalog R113; MIT; Colab notebooks). Embedding-based zero-shot
  classification for three tasks: functional vs. non-functional, NFR
  classes, and security vs. non-security.

**Relevance.** It is a cheap, deterministic, no-training baseline to run
beside the LLM on the datasets of item 1. If an embedding model matches
the LLM on those sets, that is worth knowing. If the LLM is clearly
ahead, the measurement justifies the cost of `--quality`.

### 9. Business rules as SBVR vocabulary and rules

- [paudan/sbvr-extraction](https://github.com/paudan/sbvr-extraction)
  (Danenas et al., *Data & Knowledge Engineering* 2020; Apache-2.0;
  Java, Stanford CoreNLP, OpenNLP, WordNet). It extracts SBVR business
  vocabularies (terms, fact types) and business rules from UML use-case
  diagrams.

**Relevance.** Its input does not match codetospec's, but its output
model does: SBVR is the OMG standard for business vocabulary and rules,
and codetospec already has the pieces (entities, `touches`, structured
conditions, EARS rules). An SBVR export would sit next to the Quire and
Kiro exports. The repository shows how its terms and fact types are
assembled.

### 10. Taxonomy-based concept annotation

- TT-RecS, the Taxonomic Trace Recommender (Unterkalmsteiner,
  AIRE 2020; catalog R024; Zenodo
  [3827169](https://zenodo.org/records/3827169), Apache-2.0: experiment
  instrumentation and analysis). It suggests concepts from a
  domain-specific taxonomy for each requirement, so that traceability
  runs through the taxonomy.

**Relevance.** This is the same task as `concept.Assign`, which places
each business rule in a concept of a person's model. The pilot's
method, scoring candidate concepts and letting a person confirm, is a
reference point before `concept` assignments are applied rather than
only recorded.

## Relevant ideas without public code

No source is linked in the catalog for these, and a GitHub search on
2026-11-06 found none. The papers are still worth reading:

- **Business rule classification in the Ross taxonomy** (Anish et al.,
  ICSE-SEIP 2022; R128), and **implementation-centric business-rule
  classes** (Anish et al., REW 2019; R020). These classify a business
  rule by what kind of rule it is (term, fact, constraint, derivation)
  rather than by its sentence pattern. A rule's EARS kind says nothing
  about this, and it would help the concept and nature work.
- **Extracting system states from requirements** (Pudlitz, Brokhausen
  and Vogelsang, RE 2019; R050). This is the prose side of codetospec's
  `enum` nodes and transitions: it could reveal states the rules
  mention that the code's enums do not declare.
- **GuideGen** (Hotomski and Glinz, IST 2019; R163). It generates
  guidance for updating acceptance tests when a requirement changes.
  That pairs with `drift` (stale and affected rules) and the
  `--acceptance` observables.
- **R2RS metrics** (Falessi et al., TSE 2020; R112). It predicts the
  classes a new requirement will impact from past requirement↔code
  associations. That is related to drift's `affected` and `callees`.
- **iMER** (Javed and Lin, IST 2021; R160) and **DoMoBOT** (Saini et
  al., RE 2021; R087). Both extract entity-relationship and domain
  models with traceable decisions, which compares with
  `--write-domain-model` provenance.

## Low relevance, and why

- **Story or review quality.** AQUSA
  ([RELabUU/aqusa-core](https://github.com/RELabUU/aqusa-core), MIT,
  2024; the original [gglucass/AQUSA](https://github.com/gglucass/AQUSA)
  is archived) checks user-story criteria. Its "atomic" criterion is
  already enforced by EARS one-action `shall` items.
- **Formalization into temporal logic.** ARF, DeepSTL, nlp_reform and
  Req2Spec target cyber-physical verification. codetospec's condition
  notation is its formal layer.
- **Cause-effect graph generation from pseudocode.**
  [fischJan/Automated_CEG_Creation](https://github.com/fischJan/Automated_CEG_Creation)
  (no license) builds graphs from semi-structured requirements with
  ANTLR. CiRA (item 2) covers this from prose.
- **Entity coreference.**
  [MeloFancy/DeepCoref](https://github.com/MeloFancy/DeepCoref) uses
  Chinese word vectors and Chinese training data.
- **Security and NFR advice.** FESRAS (OWASP mapping from user stories)
  and [m-h-s/RAPID](https://github.com/m-h-s/RAPID) (NFR advice for
  web APIs, CC0) are possible checklists for `facets: access` and NFR
  targets, but there is nothing to measure against.
- **The rest of the catalog.** Elicitation from app reviews, tweets,
  forums and interviews; chatbots; Jira issue linking and decision
  knowledge; story-point estimation; product-line variability; privacy
  policies and legal text; requirements-to-requirements dependency
  detection; and goal or iStar models. Their inputs have no counterpart
  in a codebase.

## Recommendations, in order

1. **Measure `--quality` on the labeled datasets of item 1.** Use the
   PROMISE→attribute table above, an eval harness modeled on
   `CODETOSPEC_EVAL`, the data downloaded rather than committed, and the
   result recorded in a new `docs/verification/` file. Add ZSL4REQ
   (item 8) as the baseline if the LLM result is ambiguous.
2. **Use CiRA as an offline check that conditions agree with their
   prose** (item 2). Script it over an existing `~/spec-graphs` output,
   comparing CiRA's cause-effect graph with each rule's structured
   condition, and count how often the logic disagrees while the values
   agree.
3. **Measure citations against TAROT's gold links** (item 3) once a Java
   grammar exists. iTrust is the usual first project in that
   literature.
4. **Score termhood of identifiers for concept proposals** (item 4) in
   Go, using `~/spec-graphs` as the reference corpus. Then **look for
   near-synonym concepts across domains** (item 5) as a question kind.
5. **Add the EARS converter's specifications and MAANA's coordination
   cases as `ears` test inputs** (items 6–7). Measure precision before
   adding a vagueness check.
6. **Read the Ross-taxonomy and system-state papers** before the next
   change to rule nature or enums.
