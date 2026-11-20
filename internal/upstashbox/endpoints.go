package upstashbox

// Every REST path, header and query key the client uses lives here. All of
// them were read from the official SDK source, @upstash/box 0.8.1 src/client.ts.

const (
	// defaultBaseURL is the API origin used when WithBaseURL is not given.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	defaultBaseURL = "https://us-east-1.box.upstash.com"

	// headerAPIKey carries the API key on every request. No Authorization
	// header is sent.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	headerAPIKey = "X-Box-Api-Key"

	// pathBoxCreate is POST, the create endpoint.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	pathBoxCreate = "/v2/box"

	// pathBoxList is GET on the box collection, returning a bare JSON array.
	// No paging parameters exist in the SDK, so List returns every box it is given.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	pathBoxList = "/v2/box"

	// queryLabel is the query key List sends to filter by label. List also
	// filters client-side, so the result is correct even if the server ignores it.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	queryLabel = "label"

	// pathBoxItem is GET (Get) and DELETE (Delete) on one box, %s is the box id.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	pathBoxItem = "/v2/box/%s"

	// pathBoxPause is POST on one box, %s is the box id. Ephemeral boxes
	// answer 400 {"error":"Ephemeral boxes cannot be paused"}.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	pathBoxPause = "/v2/box/%s/pause"

	// pathBoxResume is POST on one box, %s is the box id.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	pathBoxResume = "/v2/box/%s/resume"

	// pathBoxConnect is POST on one box, %s is the box id. The response key
	// cdp_url is the authenticated CDP WebSocket URL.
	// verified against @upstash/box 0.8.1 source on 2026-11-21
	pathBoxConnect = "/v2/box/%s/browser/connect"
)

// maxResponseBytes caps how much of a response body is read.
const maxResponseBytes = 4 << 20
