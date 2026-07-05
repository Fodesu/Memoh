local scenario = os.getenv("MEMOH_WRK_SCENARIO") or "mixed_saas_write"
local dist = os.getenv("MEMOH_WRK_DIST") or "random"

wrk.headers["Accept"] = "application/json"
wrk.headers["Content-Type"] = "application/json"

local path = "/bench/" .. scenario .. "?dist=" .. dist

function request()
  return wrk.format("POST", path, nil, "")
end

function response(status, headers, body)
  if status < 200 or status >= 300 then
    io.stderr:write("non-2xx response: " .. status .. " " .. (body or "") .. "\n")
  end
end

function done(summary, latency, requests)
  io.write(string.format("P50_LATENCY_MS %.3f\n", latency:percentile(50.0) / 1000.0))
  io.write(string.format("P90_LATENCY_MS %.3f\n", latency:percentile(90.0) / 1000.0))
  io.write(string.format("P95_LATENCY_MS %.3f\n", latency:percentile(95.0) / 1000.0))
  io.write(string.format("P99_LATENCY_MS %.3f\n", latency:percentile(99.0) / 1000.0))
end
