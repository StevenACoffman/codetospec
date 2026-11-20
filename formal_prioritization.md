# Separating MVP Requirements from Nice-to-Haves

Requirements engineering (RE) separates the requirements a Minimum Viable Product (MVP) cannot ship without from "nice to have" items in three ways: prioritization frameworks, priority attributes recorded on each requirement, and traceability back to goals. These methods are structured, but most of them are not *formal* in the mathematical sense. MoSCoW labels and Kano survey answers are still stakeholder judgments. What the frameworks add is a shared vocabulary, an explicit decision rule, and a record of who decided what and why. That makes the subjectivity visible and arguable instead of hidden.

Two things need settling before any framework is useful:

- **What the MVP is for.** In Ries's (2011) original sense, an MVP is the smallest thing you can build to test a business hypothesis and get validated learning. It is not the smallest *complete product*. So "critical for the MVP" means "needed to test the riskiest assumption" (plus whatever is legally or safely required to put it in front of users), not "needed for a polished first release."
- **Which question a framework answers.** The frameworks below answer different questions, and mixing them up is the most common error:

| Framework | Question it answers | Output |
| --- | --- | --- |
| MoSCoW | What is in scope for this timebox or release? | Scope buckets |
| Kano | How will customers react to having, or lacking, this feature? | Satisfaction categories |
| WSJF, RICE | In what order should we build things? | A ranked sequence |
| AHP, cost-value, $100 test | How do stakeholders weigh items against each other? | Relative weights |
| RFC 2119 / ISO 29148 keywords | What must a conforming implementation do? | Normative obligations |

Only MoSCoW (and story-map release slices, Section 3) draws the MVP line directly. The others feed into that decision.

## 1. Prioritization Frameworks

### MoSCoW

Created by Dai Clegg at Oracle (Clegg and Barker 1994) and adopted by the Dynamic Systems Development Method (DSDM, now the Agile Business Consortium's framework). It sorts requirements for a given timebox into four buckets:

- **Must have:** Without it, the release is unworkable, illegal, or unsafe, and there is no acceptable workaround. A useful test: "If this were missing, would we cancel the release?" If the answer is "no, we'd work around it," it is not a Must.
- **Should have:** Important and valuable, but the release still works without it, perhaps with a painful workaround.
- **Could have:** Desirable, with smaller impact. These are the first items dropped when the timebox is at risk.
- **Won't have (this time):** Explicitly out of scope *for this timebox*. Recording these is the point: it manages expectations and stops scope from creeping back in.

DSDM adds a rule that makes MoSCoW work as a decision tool rather than a labeling exercise: Musts should be **no more than about 60%** of the timebox's effort, with Coulds around 20% held as contingency. Without that cap, stakeholders label nearly everything Must, and the method stops separating anything.

### Kano Model

Noriaki Kano et al. (1984) classify features by how customer satisfaction responds to **how fully a feature is provided**. The axes are degree of fulfillment against satisfaction. The model says nothing about implementation cost. Its categories are:

- **Must-be (Threshold, Basic):** Customers take these for granted. Their absence causes strong dissatisfaction, but their presence adds no satisfaction. These are strong MVP candidates.
- **One-dimensional (Performance):** Satisfaction rises roughly in proportion to how well the feature is delivered (faster, cheaper, more storage). Whether these belong in the MVP depends on the hypothesis being tested.
- **Attractive (Excitement, Delighter):** Customers do not expect these, so their absence causes no dissatisfaction, but their presence delights. They are usually "nice to have," *unless* the delighter is the differentiator your MVP exists to test.
- **Indifferent:** Customers do not care either way. These are candidates for Won't have.
- **Reverse:** Some customers are less satisfied when the feature is present. Leave it out, or make it optional.

Categories are assigned empirically, not by opinion. A Kano survey asks each respondent a pair of questions per feature: a *functional* question ("How would you feel if the product had X?") and a *dysfunctional* one ("...if it did not have X?"). An evaluation table maps each answer pair to a category, and contradictory pairs are flagged as *Questionable*. Categories also drift over time: today's delighter becomes tomorrow's basic expectation, so a Kano classification needs periodic re-surveying.

### Weighted Shortest Job First (WSJF)

WSJF comes from Reinertsen's (2009) "cost of delay divided by duration" (CD3) scheduling rule. The Scaled Agile Framework (SAFe) popularized a relative-estimate version:

```text
WSJF = Cost of Delay / Job Size

Cost of Delay = User-Business Value + Time Criticality + Risk Reduction / Opportunity Enablement
```

Each term is estimated *relative to the other items* (usually on a modified Fibonacci scale), and Job Size stands in for duration. WSJF **sequences** work; it does not define scope. A high score means "do this sooner," not "this is in the MVP." Use it to order the Musts and Shoulds after MoSCoW (or a story map) has drawn the scope line.

### Other Scoring and Weighting Methods

- **RICE** (Intercom): `Reach × Impact × Confidence / Effort`. Like WSJF it ranks; the Confidence term discounts optimistic guesses.
- **Cost-value approach with AHP** (Karlsson and Ryan 1997): stakeholders make pairwise comparisons of requirements on value and on cost, and the Analytic Hierarchy Process turns them into ratio-scale weights with a consistency check. This is the closest of these methods to formal, but the number of comparisons grows quadratically with the number of requirements.
- **Relative weighting** (Wiegers and Beatty 2013): each requirement is scored on benefit if present, penalty if absent, cost, and risk. The priority is value divided by weighted cost and risk.
- **Cumulative voting ($100 test):** each stakeholder spreads a fixed budget across requirements. It is cheap and fast, but open to tactical voting.
- **Release planning as optimization:** the *Next Release Problem* (Bagnall et al. 2001) frames choosing a release's requirements as a constrained optimization: maximize stakeholder value within a cost budget, subject to dependencies between requirements. It is solved with integer programming or search-based methods.

## 2. Requirement Attributes and Artifacts

In a Software Requirements Specification (SRS) or a product backlog, the distinction is recorded in the requirements themselves:

- **Priority and criticality attributes.** IEEE 830-1998 recommended ranking requirements by importance (*essential*, *conditional*, *optional*) and by stability. Its successor, ISO/IEC/IEEE 29148:2018, lists requirement attributes such as priority, criticality, risk, stability, source, and rationale. Many teams use P0–P3 labels instead. Whatever the scheme, define each level by what happens if the item is omitted, and keep *priority* (stakeholder value) separate from *criticality* (consequence of failure). A low-value logging requirement can still be safety-critical.
- **Rationale and source.** Recording *why* a requirement exists, and who asked for it, is what lets a priority be challenged later. A priority without a rationale is only an opinion.
- **Traceability.** A Requirements Traceability Matrix (RTM) links each requirement backward to the goal or stakeholder need it serves, and forward to design, code, and tests. Traceability supports MVP scoping in two ways:
  - A requirement that traces to **no** goal is a gold-plating candidate and should be challenged, whatever release it is in.
  - A requirement that traces only to goals outside the MVP hypothesis is a deferral candidate.

  Traceability shows *that* a requirement is justified; it does not by itself decide *when* it ships.

### Normative Keywords Are Not Priorities

RFC 2119 keywords (`MUST`, `SHOULD`, `MAY`) are often presented as a way to mark MVP versus nice-to-have. That misreads them. RFC 2119 (BCP 14, clarified by RFC 8174) defines **conformance obligations** for implementations of a specification, mainly for interoperability:

- **`MUST` / `REQUIRED` / `SHALL`:** an absolute requirement of the specification.
- **`SHOULD` / `RECOMMENDED`:** there may be valid reasons in particular circumstances to ignore the item, "but the full implications must be understood and carefully weighed before choosing a different course." That is much stronger than "nice to have."
- **`MAY` / `OPTIONAL`:** truly optional. Implementations that include the feature and those that omit it must still interoperate.

RFC 2119 also says these keywords should be used sparingly, only where interoperation actually requires them or to limit potentially harmful behavior. RFC 8174 adds that they carry this meaning only when written in all capitals.

A requirement can be a `MUST` of the full specification and still be scheduled after the MVP. Likewise, ISO/IEC/IEEE 29148 uses "shall" for *every* binding requirement, regardless of priority. Record scope and schedule in a priority attribute, and use normative keywords only for what the requirement obliges.

## 3. Drawing the Line: User Story Mapping

Patton's (2014) user story map lays out user activities left to right in the order a user performs them. Under each activity, stories are stacked top to bottom by importance. The MVP is a **horizontal slice** across the whole map: the thinnest set of stories that lets a user complete the end-to-end journey, sometimes called a *walking skeleton*. This catches a failure that per-item scoring misses: a set of individually high-priority features that still leaves a gap in the journey, so the product cannot actually be used.

For non-functional requirements, Gilb's Planguage (Gilb 2005) expresses the same split as levels on one quantified scale rather than as separate requirements. *Fail* and *Survival* levels are constraints the release must meet; *Goal*, *Stretch*, and *Wish* levels are targets. The MVP must clear the constraints, and the targets are where the "nice to have" range lives.

## 4. Summary

| Dimension | Critical for the MVP | Important but deferrable | Nice to have |
| --- | --- | --- | --- |
| MoSCoW | Must have | Should have | Could have |
| Kano (input, not a scope label) | Must-be; any feature the MVP hypothesis depends on | One-dimensional | Attractive (unless it is the differentiator under test); Indifferent → Won't have |
| IEEE 830 importance | Essential | Conditional | Optional |
| Planguage level (NFRs) | Meets Fail / Survival constraints | Reaches Goal | Reaches Stretch / Wish |
| Impact if omitted | Release is unusable, unsafe, non-compliant, or cannot test its core hypothesis | Real pain or a workaround, but the release still works | Minor inconvenience or missed delight |

WSJF and RICE scores do not appear in this table because they order work *within* and *across* these columns rather than defining them. RFC 2119 keywords are absent because they describe conformance, not priority.

## 5. Common Failure Modes

- **Must inflation.** Everything is labeled Must. Enforce the roughly 60% effort cap and the "would we cancel the release?" test.
- **Confusing sequence with scope.** A WSJF ranking is treated as the MVP boundary.
- **Confusing conformance with priority.** `MUST` in a specification is read as "ship first."
- **Delighters dismissed by reflex.** When the MVP exists to test differentiation, the delighter *is* the hypothesis.
- **Stale classifications.** Kano categories and MoSCoW labels are set once and never revisited, even though both are tied to a point in time: the market, or a single timebox.
- **No rationale.** Priorities recorded without a source or reason cannot be renegotiated, so they quietly become permanent.

## References

- Agile Business Consortium. *MoSCoW Prioritisation* (DSDM Agile Project Framework).
- Bagnall, A. J., Rayward-Smith, V. J., and Whittley, I. M. (2001). "The next release problem." *Information and Software Technology* 43(14).
- Bradner, S. (1997). [RFC 2119: Key words for use in RFCs to Indicate Requirement Levels](https://www.rfc-editor.org/rfc/rfc2119).
- Clegg, D., and Barker, R. (1994). *Case Method Fast-Track: A RAD Approach*. Addison-Wesley.
- Gilb, T. (2005). *Competitive Engineering*. Butterworth-Heinemann.
- IEEE (1998). *IEEE Std 830-1998: Recommended Practice for Software Requirements Specifications*.
- ISO/IEC/IEEE (2018). *ISO/IEC/IEEE 29148:2018: Systems and software engineering — Life cycle processes — Requirements engineering*.
- Kano, N., Seraku, N., Takahashi, F., and Tsuji, S. (1984). "Attractive quality and must-be quality." *Journal of the Japanese Society for Quality Control* 14(2).
- Karlsson, J., and Ryan, K. (1997). "A cost-value approach for prioritizing requirements." *IEEE Software* 14(5).
- Leiba, B. (2017). [RFC 8174: Ambiguity of Uppercase vs Lowercase in RFC 2119 Key Words](https://www.rfc-editor.org/rfc/rfc8174).
- Patton, J. (2014). *User Story Mapping*. O'Reilly.
- Reinertsen, D. G. (2009). *The Principles of Product Development Flow*. Celeritas.
- Ries, E. (2011). *The Lean Startup*. Crown Business.
- Scaled Agile, Inc. [WSJF](https://framework.scaledagile.com/wsjf) (SAFe).
- Wiegers, K., and Beatty, J. (2013). *Software Requirements*, 3rd ed. Microsoft Press.
