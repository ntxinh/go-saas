package orgs

// rolePolicies are the global p-line templates seeded at boot (dom "*"
// matches any org per the model's matcher). g-lines binding a user to a
// role inside one org are written per membership by the service.
//
// owner and admin currently share every verb — admin exists so a future
// narrower policy (e.g. no DELETE /orgs) has somewhere to land.
var rolePolicies = [][]string{
	{"role:owner", "*", "/v1/orgs/*", ".*"},
	{"role:admin", "*", "/v1/orgs/*", "(GET|POST|PATCH|DELETE)"},
	{"role:member", "*", "/v1/orgs/*", "GET"},
}
