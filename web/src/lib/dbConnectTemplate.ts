// Whether a saved-connection URL is an environment template ("${DATABASE_URL}").
//
// This mirrors dbconnect.IsTemplate on the server (internal/dbconnect/expand.go)
// exactly: a BRACED reference with a shell-valid name makes it a template; a
// bare "$" (as in a password) or an invalid name stays literal. The client uses
// it to decide two things: whether a literal URL needs the master password, and
// whether `project_path` may be sent (the server stores only templates in a
// project's .ocode/db.json). Keep the two in step.
export function isEnvTemplateURL(url: string): boolean {
  return /\$\{[A-Za-z_][A-Za-z0-9_]*\}/.test(url);
}
