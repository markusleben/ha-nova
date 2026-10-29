package main

// The catalog words of a raw plan's lines (plan_raw_say.go): each step's
// verb, what a storage collection holds, and the object of every other
// allowlisted WS request.

// rawNoun: what a storage collection holds, as raw.object.item names it
// (en "toggle helper", de „den Schalter-Helfer“).
func rawNoun(lang, collection string) string {
	switch collection {
	case "input_boolean":
		return confirmT(lang, "raw.noun.input_boolean")
	case "input_number":
		return confirmT(lang, "raw.noun.input_number")
	case "input_text":
		return confirmT(lang, "raw.noun.input_text")
	case "input_select":
		return confirmT(lang, "raw.noun.input_select")
	case "input_datetime":
		return confirmT(lang, "raw.noun.input_datetime")
	case "input_button":
		return confirmT(lang, "raw.noun.input_button")
	case "counter":
		return confirmT(lang, "raw.noun.counter")
	case "timer":
		return confirmT(lang, "raw.noun.timer")
	case "schedule":
		return confirmT(lang, "raw.noun.schedule")
	case "person":
		return confirmT(lang, "raw.noun.person")
	case "zone":
		return confirmT(lang, "raw.noun.zone")
	case "tag":
		return confirmT(lang, "raw.noun.tag")
	case "config/area_registry":
		return confirmT(lang, "raw.noun.area")
	case "config/floor_registry":
		return confirmT(lang, "raw.noun.floor")
	case "config/label_registry":
		return confirmT(lang, "raw.noun.label")
	case "config/category_registry":
		return confirmT(lang, "raw.noun.category")
	}
	panic("raw line: unknown collection " + collection)
}

// rawVerb: a raw step's verb, which opens its line in English and ends it
// in German (raw.goal), and names its button (actionGoal.ownButton).
func rawVerb(lang, id string) string {
	switch id {
	case "delete":
		return confirmT(lang, "raw.verb.delete")
	case "write":
		return confirmT(lang, "raw.verb.write")
	case "save":
		return confirmT(lang, "raw.verb.save")
	case "change":
		return confirmT(lang, "raw.verb.change")
	case "remove":
		return confirmT(lang, "raw.verb.remove")
	case "create":
		return confirmT(lang, "raw.verb.create")
	case "enable":
		return confirmT(lang, "raw.verb.enable")
	case "disable":
		return confirmT(lang, "raw.verb.disable")
	case "ignore":
		return confirmT(lang, "raw.verb.ignore")
	case "rename":
		return confirmT(lang, "raw.verb.rename")
	case "show_again":
		return confirmT(lang, "raw.verb.show_again")
	case "expose":
		return confirmT(lang, "raw.verb.expose")
	case "stop_exposing":
		return confirmT(lang, "raw.verb.stop_exposing")
	case "turn_on":
		return confirmT(lang, "raw.verb.turn_on")
	case "turn_off":
		return confirmT(lang, "raw.verb.turn_off")
	case "import":
		return confirmT(lang, "raw.verb.import")
	case "continue":
		return confirmT(lang, "raw.verb.continue")
	case "render":
		return confirmT(lang, "raw.verb.render")
	case "check":
		return confirmT(lang, "raw.verb.check")
	case "cancel":
		return confirmT(lang, "raw.verb.cancel")
	case "start":
		return confirmT(lang, "raw.verb.start")
	case "submit":
		return confirmT(lang, "raw.verb.submit")
	case "reconfigure":
		return confirmT(lang, "raw.verb.reconfigure")
	case "add":
		return confirmT(lang, "raw.verb.add")
	case "open":
		return confirmT(lang, "raw.verb.open")
	case "correct":
		return confirmT(lang, "raw.verb.correct")
	case "install":
		return confirmT(lang, "raw.verb.install")
	case "uninstall":
		return confirmT(lang, "raw.verb.uninstall")
	case "pick":
		return confirmT(lang, "raw.verb.pick")
	case "show_or_hide":
		return confirmT(lang, "raw.verb.show_or_hide")
	case "refresh":
		return confirmT(lang, "raw.verb.refresh")
	case "hide":
		return confirmT(lang, "raw.verb.hide")
	case "acknowledge":
		return confirmT(lang, "raw.verb.acknowledge")
	case "make_preferred":
		return confirmT(lang, "raw.verb.make_preferred")
	case "get":
		return confirmT(lang, "raw.verb.get")
	case "fetch":
		return confirmT(lang, "raw.verb.fetch")
	case "reorder":
		return confirmT(lang, "raw.verb.reorder")
	case "send":
		return confirmT(lang, "raw.verb.send")
	case "confirm":
		return confirmT(lang, "raw.verb.confirm")
	}
	panic("raw line: unknown verb " + id)
}

// rawWSPhrases: the verb and object of every other allowlisted WS request;
// its named targets (rawNamer.targets) take their place in the object.
var rawWSPhrases = map[string]func(lang, targets string) (verb, object string){
	"lovelace/dashboards/update": func(l, t string) (string, string) {
		return "change", confirmT(l, "raw.ws.dashboard_settings", "targets", t)
	},
	"lovelace/resources/create": func(l, t string) (string, string) { return "add", confirmT(l, "raw.ws.resource", "targets", t) },
	"lovelace/resources/update": func(l, t string) (string, string) { return "change", confirmT(l, "raw.ws.resource", "targets", t) },
	"lovelace/resources/delete": func(l, t string) (string, string) { return "remove", confirmT(l, "raw.ws.resource", "targets", t) },
	"recorder/update_statistics_metadata": func(l, t string) (string, string) {
		return "rename", confirmT(l, "raw.ws.statistic", "targets", t)
	},
	"recorder/clear_statistics": func(l, t string) (string, string) { return "delete", confirmT(l, "raw.ws.statistics", "targets", t) },
	"recorder/adjust_sum_statistics": func(l, t string) (string, string) {
		return "correct", confirmT(l, "raw.ws.statistic_sum", "targets", t)
	},
	"backup/generate": func(l, t string) (string, string) { return "create", confirmT(l, "raw.ws.backup_new", "targets", t) },
	"backup/generate_with_automatic_settings": func(l, t string) (string, string) {
		return "create", confirmT(l, "raw.ws.backup_saved_settings", "targets", t)
	},
	"backup/delete":            func(l, t string) (string, string) { return "delete", confirmT(l, "raw.ws.backup", "targets", t) },
	"hacs/repositories/remove": func(l, t string) (string, string) { return "remove", confirmT(l, "raw.ws.hacs_custom", "targets", t) },
	"hacs/repository/download": func(l, t string) (string, string) { return "install", confirmT(l, "raw.ws.hacs", "targets", t) },
	"hacs/repository/remove":   func(l, t string) (string, string) { return "uninstall", confirmT(l, "raw.ws.hacs", "targets", t) },
	"hacs/repository/version":  func(l, t string) (string, string) { return "pick", confirmT(l, "raw.ws.hacs_version", "targets", t) },
	"hacs/repository/beta": func(l, t string) (string, string) {
		return "show_or_hide", confirmT(l, "raw.ws.hacs_beta", "targets", t)
	},
	"hacs/repository/refresh": func(l, t string) (string, string) { return "refresh", confirmT(l, "raw.ws.hacs_refresh", "targets", t) },
	"hacs/repository/ignore":  func(l, t string) (string, string) { return "hide", confirmT(l, "raw.ws.hacs_update", "targets", t) },
	"hacs/critical/acknowledge": func(l, t string) (string, string) {
		return "acknowledge", confirmT(l, "raw.ws.hacs_notice", "targets", t)
	},
	"config/auth/create":              func(l, t string) (string, string) { return "create", confirmT(l, "raw.ws.account", "targets", t) },
	"config/auth/delete":              func(l, t string) (string, string) { return "delete", confirmT(l, "raw.ws.account", "targets", t) },
	"assist_pipeline/pipeline/create": func(l, t string) (string, string) { return "create", confirmT(l, "raw.ws.assistant", "targets", t) },
	"assist_pipeline/pipeline/update": func(l, t string) (string, string) { return "change", confirmT(l, "raw.ws.assistant", "targets", t) },
	"assist_pipeline/pipeline/delete": func(l, t string) (string, string) { return "delete", confirmT(l, "raw.ws.assistant", "targets", t) },
	"assist_pipeline/pipeline/set_preferred": func(l, t string) (string, string) {
		return "make_preferred", confirmT(l, "raw.ws.assistant_preferred", "targets", t)
	},
	"homeassistant/expose_entity": func(l, t string) (string, string) { return "change", confirmT(l, "raw.ws.exposed", "targets", t) },
	"calendar/event/create":       func(l, t string) (string, string) { return "add", confirmT(l, "raw.ws.event_to", "targets", t) },
	"calendar/event/update":       func(l, t string) (string, string) { return "change", confirmT(l, "raw.ws.event_in", "targets", t) },
	"calendar/event/delete":       func(l, t string) (string, string) { return "delete", confirmT(l, "raw.ws.event_from", "targets", t) },
	"todo/item/move":              func(l, t string) (string, string) { return "reorder", confirmT(l, "raw.ws.todo_item", "targets", t) },
	"blueprint/save":              func(l, t string) (string, string) { return "save", confirmT(l, "raw.ws.blueprint", "targets", t) },
	"blueprint/delete":            func(l, t string) (string, string) { return "delete", confirmT(l, "raw.ws.blueprint", "targets", t) },
	"blueprint/import":            func(l, t string) (string, string) { return "fetch", confirmT(l, "raw.ws.blueprint_from", "targets", t) },
	"camera/stream":               func(l, t string) (string, string) { return "get", confirmT(l, "raw.ws.stream_link", "targets", t) },
	"media_source/resolve_media":  func(l, t string) (string, string) { return "get", confirmT(l, "raw.ws.media_link", "targets", t) },
}
