---
read_when:
  - reusing OpenClaw profile names and avatars in ClickClack
  - linking existing ClickClack accounts to an OpenClaw deployment
---

# OpenClaw identity mapping

Operators can link existing ClickClack human accounts to OpenClaw profiles and
reuse their names and avatars. The mapping keeps the ClickClack user ID, message
authors, memberships, handles, and login identities intact. It uses the existing
identity store and requires no schema or server configuration change.

Export the `profiles` result from an authenticated OpenClaw `users.list` call.
The file must contain the result object, without an RPC response wrapper:

```json
{
  "profiles": [
    {
      "id": "profile-example",
      "displayName": "Example Person",
      "emails": ["person@example.com"],
      "mergedInto": null
    }
  ]
}
```

Run the command on the ClickClack server with access to its existing database:

```sh
clickclack admin identity sync \
  --data /var/lib/clickclack \
  --source https://control.example.com \
  --file /path/to/profiles.json
```

Use `--db` for a database URL, including Postgres. Omitting `--file`, or passing
`--file -`, reads JSON from standard input. Treat the export as private account
data; keep it outside the repository, restrict its permissions, and delete it
after use. Import only an authenticated export from the origin named by
`--source`: this is an operator command, not a public identity assertion API.

The first sync matches case-insensitive aliases against existing users. OpenClaw's
`emails` field can also contain opaque login aliases; these are matched exactly
after trimming and lowercasing, or reported as unmatched when absent in ClickClack.
It records the canonical source origin and profile ID as a new identity attached
to the matched user. Later syncs use that mapping even when source emails change.
Imported aliases do not become new ClickClack login identities. The command
rejects bot matches, ambiguous email matches, conflicting aliases, and attempts
to reassign an existing mapping. It validates the complete import before
writing, and applies all links and profile changes in one transaction.

Profiles without an existing matching account are reported and skipped; sign in
to ClickClack normally, then rerun the command to link new users. Merged source
profiles are skipped. A merged profile that would replace an already-linked
source ID needs an explicit operator resolution rather than silently moving an
identity between users.

Nonempty source display names replace ClickClack display names. Empty names
leave the current name intact. Blank avatars and email-generated Gravatar
fallbacks become the stable source URL:

```text
https://control.example.com/api/users/profile-example/avatar
```

OpenClaw continues to own and serve the image, so future avatar uploads appear
without another import. Explicit ClickClack avatar URLs remain unchanged. To
return an explicit override to OpenClaw, clear it in profile settings and rerun
the sync. Avatar URLs contain no credentials: viewers must also be authenticated
to the source. Keep both applications same-site and verify the source image
loads from ClickClack before importing. The command does not publish protected
avatars, copy image bytes, or change either service's authentication policy.

Output is a JSON report: `linked` counts new mappings, `updated` counts changed
profiles, and `unchanged` counts already-linked profiles requiring no change.
`unmatched_profile_ids` lists accounts to revisit, and `merged_skipped` counts
merged profiles. A new link can also count as an update. Reload open ClickClack
views to hydrate historical messages with the new names and avatar URLs.

Imports are bounded to 4 MiB, 10,000 profiles, and 64 email aliases per profile.
Display names must fit ClickClack's existing 80-byte limit. Invalid input fails
before opening the database; database conflicts roll back the entire import.
