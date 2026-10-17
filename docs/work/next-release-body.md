# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## New Features

- A room or device command that reaches exactly one matching device ("turn
  off the kitchen lights" with one light there) now runs right away and is
  verified, exactly like naming that device. Rooms with several matching
  devices and groups still show a preview first.
- **Faster setup on extra computers:** when the NOVA Relay App already runs on
  your Home Assistant, setup skips the install steps and goes straight to
  pairing this computer.
- **Sign in with Home Assistant to pair:** setup opens your Home Assistant
  login in the browser. Sign in as the owner, and setup connects this computer
  by itself, with its own connection that you can remove in NOVA at any time.
  Over SSH, with an older NOVA Relay App, or when sign-in does not work out
  (for example another Home Assistant user), setup uses the one-time six-digit
  code from NOVA as before.
- **One install link:** setup opens the NOVA Relay App page directly and adds
  the repository on the way — click "Add", then Install and Start.
- **A first look right after pairing:** setup ends with your home in one
  line, up to two things worth a look (a low battery, an automation that
  can't run), and example prompts that use your own room and device names.
- **Know your home with exact numbers:** ask "what do I have?", "where is
  this sensor used?", or "review my whole setup" — rooms, devices,
  automations, integrations, unavailable entities, and low batteries are
  counted by HA NOVA itself, so the AI never guesses a number or a name.
- **Undo a deleted automation or script:** "undo that" right after a delete
  restores it from the copy saved before the delete, under its original
  id, with the usual preview. It never replaces an item that exists.

- **Replace a device or a phone:** "my door sensor died, I paired a new
  one" or "I have a new phone" moves automations, scripts, scenes,
  dashboards, helpers, energy sources, notifications, and presence to the new
  device in one reviewed plan. When the old device is gone for good, the new
  one takes over its entity ids, so history and statistics continue.
- **Build whole dashboards:** ask for a dashboard or a view and HA NOVA
  builds it, reorders or removes views, and uses custom cards you have
  installed. The preview lists every view and card that changes; a copy is
  saved before each save, and "undo that" restores the previous version.
  "Check my dashboards" lists cards that point at missing entities or at
  custom cards that are not installed.
- **Move your rules to Home Assistant:** paste a Node-RED, Homey,
  SmartThings, Hubitat, or openHAB rule, or point at old automation YAML, and
  HA NOVA turns it into a native automation. It first lists what the rule
  does, maps each device to a real Home Assistant entity (or asks), and names
  anything it cannot translate. The old rule stays disabled, not deleted,
  until the new one has been checked against that list.
- **ESPHome device configs:** ask what a device in `/config/esphome/` does,
  or to check it, and HA NOVA explains it in plain words and flags keys that
  newer ESPHome releases removed, pins that cannot do what the file asks, and
  passwords written into the file. With file access turned on, edits show the
  changed part first and save a copy. Passwords and keys are never shown. Installing on the device stays
  in the ESPHome App; afterwards HA NOVA checks that the device's entities
  came back.

## What To Watch

- **Shorter one-time codes:** unlocking a door, opening a gate or garage,
  disarming the alarm, and deletes now ask for a four-digit one-time code from
  HA NOVA ("Type 4821 to unlock “Front Door”") instead of a long
  `confirm:…` code. A plain "yes" still can't confirm, and a code works only
  for its own plan. A plan shown before the update has to be made again.
- **Confirm on your computer, not in chat:** where your computer can show
  one, these actions now open a confirmation window — click Confirm there, or
  Cancel. This also replaces the confirmation form in apps that show one; the
  form stays where no window can open. Your AI assistant never sees a code. It still can click the window
  if you allow it to control your screen (computer use, or apps you granted
  Accessibility), and on Windows and some Linux desktops other programs can
  too. Over
  SSH, without a desktop, or where the window can't open (some AI sandboxes),
  HA NOVA shows the one-time code in chat as before and says that the
  assistant can see it too. To turn the window off: `ha-nova setup
  confirm-window off` (you confirm that in the window).

- `ha-nova uninstall` no longer guides removal of the NOVA Relay App by
  default. It first asks whether this is the last computer using HA NOVA,
  because removing the App disconnects every paired computer.
- Pressing a button, turning on a switch, or opening a cover on a device
  that also has a lock, alarm, or access cover now needs a one-time code. Configuration
  and diagnostic buttons there (ping, identify, restart) work as before.
- HA NOVA now only makes the low-level changes it has a checked rule for.
  Settings changes through an integration's own interface (Z-Wave, Zigbee,
  Alarmo, and others) are pointed to the Home Assistant UI instead, so none
  of them can open a lock or disarm an alarm without the one-time code.

## Bug Fixes

- Room and device commands now skip hidden entities and configuration or
  diagnostic entities, as Home Assistant's own room targeting does; when that
  leaves nothing, the answer names the skipped entities. A device command
  also reaches the device's child devices, as Home Assistant does.
