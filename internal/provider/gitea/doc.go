// Package gitea resolves versions from a Gitea or Forgejo forge's tags and
// releases. One provider serves every instance of the API: Codeberg (the default
// host), forgejo.org, and any self-managed Gitea/Forgejo, selected by the host
// key. Discovery is a single cached REST listing per marker; selection, cooldown,
// and filtering stay with the framework.
package gitea
