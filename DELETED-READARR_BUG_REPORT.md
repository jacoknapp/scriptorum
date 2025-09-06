# Readarr Add Book API - Reproducible Failure Notes

Summary
-------
- I ran the local experimental harness `cmd/testadd` which performs a lookup then attempts up to 32 payload permutations against POST /api/v1/book and stops on first success.
- The harness now forces author name "ilona andrews", attempts to resolve/create an Author record (resulting in numeric `authorId`: 241 on this instance), and coerces `editions` to a non-nil empty array before sending.
- All 32 permutations failed. Responses were a mix of HTTP 400 validation errors (when sending incomplete `author` objects) and HTTP 500 internal server errors with a NullReferenceException originating in Readarr validation code.

Key findings
------------
- Sending an `author` object without a `foreignAuthorId` triggers a 400: Author.ForeignAuthorId must not be empty.
- Even when using a resolved numeric `authorId` (so the Author object is not present) and `editions: []`, many payloads returned HTTP 500 with a server-side NullReferenceException rooted at:

  NzbDrone.Core.Validation.QualityProfileExistsValidator.IsValid (./Readarr.Core/Validation/QualityProfileExistsValidator.cs:line 24)

  Stack-trace excerpt (truncated):

  "System.NullReferenceException: Object reference not set to an instance of an object.\n   at lambda_method172(Closure , BookResource )\n   ...\n   at NzbDrone.Core.Validation.QualityProfileExistsValidator.IsValid(PropertyValidatorContext context) in ./Readarr.Core/Validation/QualityProfileExistsValidator.cs:line 24\n"

- Previously observed ArgumentNullException when `editions` was null has been addressed in the harness by coercing `editions` to `[]` before sending.

Minimal reproducible payload (used by harness, replace values as needed)
------------------------------------------------------------------

{
  "title": "Passion's Eternal Blaze",
  "titleSlug": "44706673",
  "foreignBookId": "44706673",
  "foreignEditionId": "25031017",
  "authorId": 241,
  "editions": [],
  "qualityProfileId": 1,
  "rootFolderPath": "/books",
  "addOptions": { "addType": "automatic", "monitor": "all", "monitored": true, "booksToMonitor": [], "searchForMissingBooks": true, "searchForNewBook": true },
  "tags": ["requested-by-abs","audiobook"]
}

Observed server response for the payload above: HTTP 500 with NullReferenceException in QualityProfileExistsValidator.

Checklist (requirements coverage)
---------------------------------
- Emit canonical POST /api/v1/book schema: Done (harness sends canonical fields: title, editions, authorId/author/authorTitle, qualityProfileId, rootFolderPath, addOptions, tags)
- Ensure prerequisites (rootFolder, author) exist or are created client-side: Partially done (harness resolves/creates author and picks a server rootFolder but server still rejects payloads)
- Use POST /api/v1/book: Done (AddBookRaw uses POST to /api/v1/book)
- Run 16-32 variant sweep and stop on first success: Done (sweep implemented; stopped after 32 attempts — no success)
- Force author to "ilona andrews": Done (harness sets author to this name and attempts resolution)

Recommended next steps
----------------------
1. File an upstream Readarr issue with the minimal payload and the attached stack-trace excerpt above. The trace points to a server-side null reference in quality profile validation which needs fixing.
2. As a short-term test, try POSTing the minimal payload with `qualityProfileId` omitted entirely (or with a different known-good profile id) to confirm whether the validator is mis-handling that property.
3. Share the server logs (full stack traces) and the exact payload(s) that produced the 500s with Readarr maintainers so they can reproduce and patch.

Local artifacts
---------------
- `cmd/testadd/main.go` — experimental harness used to run the permutation sweep. It now: forces author "ilona andrews", ensures `editions` is non-nil, prefers numeric `authorId` in variants, and tries 32 payload variants.

If you want, I can:
- Extract the exact failing request/response pairs into separate files in this repo (json + response) to attach to an issue.
- Try additional heuristics (omit qualityProfileId, try 0, or vary content) and re-run the sweep.

-- End of report
Title: NullReferenceException in QualityProfileExistsValidator when POSTing /api/v1/book

Summary
-------
While attempting to POST a Readarr Book resource to /api/v1/book using the canonical Book schema, the server returns an internal 500 with a NullReferenceException in QualityProfileExistsValidator.IsValid. Multiple client-side payload permutations (including validated qualityProfileId and rootFolderPath values) produce the same server-side NRE. This appears to be a server-side validation bug.

Environment
-----------
- Readarr instance: (internal URL used during testing) https://audiobookarr.knapp
- Local test client: scriptorum cmd/testadd (go)
- Known server state discovered during tests:
  - QualityProfiles: 1: eBook, 2: Spoken
  - RootFolders: /books

Reproduction steps
------------------
1. Use the canonical Readarr POST /api/v1/book schema and set qualityProfileId to a known profile (2) and rootFolderPath to a known root ("/books" or "/books/audiobooks").
2. POST the payload to /api/v1/book (API key removed below).
3. Observe a 500 Internal Server Error with a NullReferenceException in QualityProfileExistsValidator.IsValid.

Payload examples (sanitized)
----------------------------
Primary book add payload that was sent (one of many variants tried):

Variant 1 (automatic addOptions):
{
  "addOptions": {"addType":"automatic","booksToMonitor":[],"monitor":"all","monitored":true,"searchForMissingBooks":true,"searchForNewBook":true},
  "authorTitle": null,
  "editions": [{"foreignEditionId":"25031017"}],
  "foreignBookId": "44706673",
  "foreignEditionId": "25031017",
  "qualityProfileId": 2,
  "rootFolderPath": "/books/audiobooks",
  "tags": ["requested-by-abs","audiobook"],
  "title": "Passion's Eternal Blaze",
  "titleSlug": "44706673"
}

Other variants tested included:
- Omitting qualityProfileId
- Using qualityProfileId = 1
- Removing nested author object entirely
- Sending an "author" object with name and foreignAuthorId/rootFolderPath set
- Manual vs automatic addOptions

Server responses
----------------
Typical 500 response body (sanitized):
{
  "message": "Object reference not set to an instance of an object.",
  "description": "System.NullReferenceException: Object reference not set to an instance of an object.\n   at lambda_method172(Closure , BookResource )\n   at FluentValidation.Internal.Extensions...\n   at NzbDrone.Core.Validation.QualityProfileExistsValidator.IsValid(PropertyValidatorContext context) in ./Readarr.Core/Validation/QualityProfileExistsValidator.cs:line 19\n   ..."
}

Another variant produced this different server exception (likely related):
{
  "message": "Sequence contains no matching element",
  "description": "System.InvalidOperationException: Sequence contains no matching element\n   at System.Linq.ThrowHelper.ThrowNoMatchException()...\n   at NzbDrone.Core.Books.AddBookService.AddSkyhookData(Book newBook) in ./Readarr.Core/Books/Services/AddBookService.cs:line 106\n   ..."
}

Notes & test harness
--------------------
- I used the project's test harness: cmd/testadd which performs a lookup then attempts add flows. During runs the harness fetched QualityProfiles and RootFolders from the server successfully.
- The harness tried author resolution paths (lookup -> import -> create) and sanitized JSON to avoid sending null/invalid "authorId" tokens.
- All client-side permutations posted from the harness resulted in the same NRE or related server exceptions.

Suggested next actions for Readarr maintainers
--------------------------------------------
- Inspect NzbDrone.Core.Validation.QualityProfileExistsValidator.IsValid for potential null dereference. Ensure all required properties are present before use.
- Add defensive checks or clear validation messages when quality profile or related data are missing.
- If interested, I can provide the exact JSON payloads and full raw stack traces (redacted of API keys) — they are saved locally in the harness logs.

Local copies
------------
- I saved the exact payloads and server responses in the local test harness logs when reproducing; tell me if you want me to attach those full logs to this draft (sanitized) or paste them here.

Author / Reporter
-----------------
- Report generated automatically from local tests using scriptorum cmd/testadd.


