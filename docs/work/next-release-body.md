# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Phone confirmations: a plain tap no longer loses the question

- A plain tap on an HA NOVA question that the NOVA page can approve (one the NOVA App holds for a computer) opens that request on the NOVA page in the Home Assistant app. Approve there as on the NOVA page. On Android the notification also stays in the list.
- On Android, a plain tap on any other HA NOVA question does nothing, and the notification stays: pull it down for its buttons.
- On an iPhone, any other unanswered question comes back once, quietly (no sound), if enough time is left: a plain tap there removes it without answering.
- The notification that links a phone now explains how to answer (press and hold on an iPhone, pull down on Android). If nobody answers a question that had no NOVA page link, the phone's result says how to answer next time. On an iPhone it also names the optional Home Assistant app setting "Show actions when tapped" (app 2026.9.3 or later), which shows the buttons of change requests on a plain tap.
- The NOVA page opened from a notification shows that request alone. Once it has ended, the page says so instead of showing another request in its place.
- README, "You approve it", phone bullet: add "A plain tap doesn't answer: for a request from a computer it opens that request on the NOVA page; otherwise it does nothing on Android, and on an iPhone the question comes back once."
