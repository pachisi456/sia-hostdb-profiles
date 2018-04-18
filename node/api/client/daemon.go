package client

import "github.com/pachisi456/sia-hostdb-profiles/node/api"

// DaemonVersionGet requests the /daemon/version resource
func (c *Client) DaemonVersionGet() (dvg api.DaemonVersionGet, err error) {
	err = c.get("/daemon/version", &dvg)
	return
}

// DaemonStopGet stops the daemon using the /daemon/stop endpoint.
func (c *Client) DaemonStopGet() (err error) {
	err = c.get("/daemon/stop", nil)
	return
}
