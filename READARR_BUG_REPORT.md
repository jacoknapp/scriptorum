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


