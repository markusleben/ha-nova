if .ok and (.data | type == "object") then ((.data.automation // []) + (.data.script // [])) else error("search/related failed: \(.error.message // "unexpected response shape")") end
