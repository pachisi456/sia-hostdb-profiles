package client

import "github.com/pachisi456/sia-hostdb-profiles/node/api"

// HostDbActiveGet requests the /hostdb/active endpoint's resources
func (c *Client) HostDbActiveGet() (hdag api.HostdbActiveGET, err error) {
	err = c.get("/hostdb/active", &hdag)
	return
}
