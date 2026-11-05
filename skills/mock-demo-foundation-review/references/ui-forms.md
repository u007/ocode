# UI Forms & Lists Checklist

Apply to every form and list in the inventory. The UI's job is to make the right data
easy to enter and the wrong data hard; the database's job is to refuse the wrong data
anyway. Check both halves.

Contents: 1 Form-to-schema parity · 2 Entry normalization · 3 Validation · 4 Duplicate
UX · 5 Submit safety · 6 Pickers & lookups · 7 Dates, money, locale · 8 Lists & tables ·
9 Bulk paths · 10 Accessibility & cognitive load

## 1. Form-to-schema parity

- Every field on the form maps to a column, a relationship, or an explicit "not stored"
  decision. A field with no home is a finding.
- Every `NOT NULL` column without a default is set by some form, import or process.
- Required markers on the form agree with the model. Optional-on-form but
  required-in-model forces fake values; required-on-form but nullable-in-model invites
  bypass.
- Field types match end to end. Check every field against this matrix; a mismatch in
  any column is a finding.

  | Data | Input control | Client check | Server / DB type |
  |------|---------------|--------------|------------------|
  | Count, quantity | `type=number`, `step=1`, min/max | integer, range | integer + CHECK |
  | Amount of money | currency input (masked), 2 dp | decimal, currency code | integer minor units + currency |
  | Percentage, measure | `type=number` with unit shown | range, unit | numeric + CHECK |
  | Date only | date picker | valid date, range | `date` |
  | Date and time | datetime picker with zone | valid instant | `timestamptz` (UTC) |
  | Email | `type=email`, autocomplete | format, max 320 | varchar(320) + normalized column |
  | Phone | tel input with country | parses to E.164 | varchar + `phone_e164` |
  | URL | `type=url` | scheme + host | varchar(2048) |
  | Yes / no | checkbox, switch, radio pair | boolean | boolean NOT NULL default |
  | Closed set (status, type) | select / radio / segmented | value in set | enum or CHECK |
  | Managed list (category, region) | searchable select | id exists | FK to lookup table |
  | Reference to entity | searchable picker bound to id | id exists, in tenant | FK |
  | Short text (name, code) | text input with maxlength | trimmed, length | varchar(n) |
  | Long text | textarea with counter | length cap | text with cap |
  | File | file input with accept | type, size | metadata row + object store |
  | Password, secret | password input | strength policy | hash / vault, never plain |

  Rules behind the matrix: the control constrains what can be typed, the client check
  explains mistakes immediately, the server repeats the check, and the column type
  makes a bypass impossible. A text input for a number, date, enum or id, or a text
  column behind a typed input, fails the row.
- The edit form shows the same fields as create, or documents why not. Fields that exist
  only on create produce rows that can never be corrected.

## 2. Entry normalization (on the write path, not only in the browser)

- Trim whitespace; collapse internal runs where the field is a key.
- Case-fold emails and codes for matching; keep the display form.
- Phones to E.164 with the country inferred from tenant settings or an explicit picker.
- Names: preserve as typed; generate a match key separately.
- Numbers: strip thousands separators, accept locale decimal marks consciously.
- URLs: scheme added, host lowercased.
- Never normalize passwords or free-text notes.

## 3. Validation

- Every rule exists twice: client-side for immediate feedback, server-side as the
  source of truth. A rule that exists only in the browser is a finding.
- Errors sit next to the field, name the rule ("must be a valid email"), and keep the
  user's input. Errors in a toast or at the top only are a cognitive-load finding.
- Format rules are forgiving on input and strict on storage (accept "(555) 123-4567",
  store `+15551234567`).
- Cross-field rules (end after start, total equals sum of lines) are validated on the
  server and shown on the form.
- The server returns structured field errors the form can map; a bare 500 or a generic
  "something went wrong" is a finding.

## 4. Duplicate UX

- Create forms for keyed entities search as the key field is typed and surface matches
  with a "use this one" action.
- Near matches on submit produce an interstitial listing the candidates, with "create
  anyway" as an explicit choice that is recorded (`duplicate_override_by`).
- Exact matches are refused with a link to the existing record, never silently merged
  and never allowed through.
- "Add new" from inside a picker goes through the same duplicate check as the main form.

## 5. Submit safety

- The submit button disables during the request and the request carries an idempotency
  key, so a double-click, a slow network or a retry creates one row.
- Destructive actions (delete, cancel, refund, bulk anything) confirm with the object
  named ("Delete invoice #1042?") and are reversible where the model allows.
- Long forms autosave a draft or warn before navigation discards input.
- A successful save lands somewhere that proves it (the record page, a row highlighted
  in the list), not a blank form with a toast.
- Edit saves send the version the form loaded; a stale version shows "changed by X,
  reload" instead of overwriting.

## 6. Pickers & lookups

- Any field that references another entity is a searchable picker bound to the id.
- Enumerations are selects, radios or segmented controls fed from the canonical list;
  never free text with placeholder hints.
- Pickers show enough to disambiguate (name + email, SKU + description), not the name
  alone.
- Large lists paginate server-side with search; a dropdown of 5,000 customers is a
  finding.
- "Other / please specify" writes to a distinct column, not into the enum.

## 7. Dates, money, locale

- Date-only fields use date inputs and store dates; date-time fields capture a timezone
  or use the tenant's.
- Money fields show the currency, accept the user's locale, store minor units.
- Relative display ("3 days ago") always has an absolute value on hover or beside it.
- Units are displayed with values and chosen once per tenant or per field, never mixed.

## 8. Lists & tables

- Every list is server-side sorted and paginated, with the sort and page in the URL.
  A mock that fetches everything and sorts in the browser is a scalability finding.
- Default sort is deliberate and stated (newest first, or by name).
- Filters map to indexed columns; free-text search names the fields it searches.
- Columns show ids or stable references where a user may need to cite a row.
- Bulk selection and bulk actions obey the same confirmation and permission rules as
  single actions.
- Empty states say why the list is empty and what to do (no results vs no data vs no
  permission).
- Counts shown next to lists match what the filter returns, or are labelled as totals.

## 9. Bulk paths (import, export, API)

- Import previews rows, reports per-row errors, and runs the same uniqueness and
  validation rules as the form. Partial success is explicit (N created, M skipped, with
  a downloadable error file).
- Import records a `source` and a batch id on every row so a bad batch can be found and
  reversed.
- Export includes ids and timestamps, not just display fields, so the data can be
  re-imported or reconciled.
- Any API the demo exposes enforces the same rules; a mock backend that accepts anything
  is a finding when the plan is to keep it.

## 10. Accessibility & cognitive load

- Every input has a visible label; placeholder text is not a label.
- Keyboard-only completion of every form works; focus moves to the first error.
- Related fields are grouped and ordered as the user thinks about them, not as the
  table is laid out.
- One primary action per screen; secondary actions look secondary.
- Defaults are filled where the system can know the answer (tenant, today, current
  user, last-used value) so the user types only what the system cannot know.
- The form length matches the entity's importance: a five-field quick-create plus a
  full edit page beats one twenty-field wall.
- Colour is never the only carrier of state; status has text or an icon.
