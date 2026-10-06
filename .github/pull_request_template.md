## What and why

Describe the user problem and what this change does about it. Say why this approach rather
than another one, and name anything you deliberately did not do.

## Verification

State exactly what was run and what it returned, with the four numbers — passed, failed,
skipped, did not run — and the command. "Tests pass" is not a result.

- Command(s):
- Result:

If something in this change was **written and not run**, say so here, plainly, and name the
suite that must confirm it. Do not describe an unrun change as verified, passing or green.
If a check could not be run at all, say why: an environment limitation is a fact worth
recording, not a failure to hide.

## Risk

- **Blast radius:** which paths this touches. The authority and admission ordering, the
  shared conversation pipeline and the media engine are the ones to be explicit about.
- **Migrations or data:** does this change what is stored? A schema change needs its own
  justification, a forward-only migration and a rehearsed upgrade. Migration numbers are
  never reused.
- **Backups:** does this change what a backup contains, or what a restore accepts?
- **Reversibility:** can this be rolled back, and what does rolling back lose?

## Checklist

- [ ] The change is focused: one concern, and no reformatting of unrelated lines.
- [ ] Permissions are enforced on the server, not only in the interface, and a new surface
      shows only what its caller may already read.
- [ ] A behaviour change has a test beside it, and a regression fails without the fix.
- [ ] Documentation is updated where behaviour, limits or operator steps changed.
- [ ] No credentials, setup tokens, private conversations, database files or backups are
      included in the diff, the description or any attachment.
- [ ] The CLA sentence is in this description (see `CLA.md`), and the signatory record is
      part of this pull request.
- [ ] Security reports are **not** made here: use the private channel in `SECURITY.md`.

## Remaining limits and risks

What this does not fix, what it does not prove, and what a reviewer should look at hardest.
A change that ships with a known limitation and says so is a better change than one that
ships with a claim it cannot support.
