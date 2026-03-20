if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
