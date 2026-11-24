# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

- 0.36 (App and CLI): a computer connected with an administrator's code works only while that person is still an administrator; a chat app's change uses the user's Home Assistant role as it is when the change runs (MAR-170, MAR-181). Computers an administrator connected before 0.36 keep their access: revoke and reconnect them to get the check. Rolling the App back leaves the computers connected by an administrator in 0.36 without access; connect them again.

