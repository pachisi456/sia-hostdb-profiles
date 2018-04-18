package client

import (
	"net/url"
	"strconv"

	"github.com/pachisi456/sia-hostdb-profiles/node/api"
)

// HostAnnouncePost uses the /host/announce endpoint to announce the host to
// the network
func (c *Client) HostAnnouncePost() (err error) {
	err = c.post("/host/announce", "", nil)
	return
}

// HostAcceptingContractsPost uses the /host endpoint to change the acceptingcontracts field of the host's settings
func (c *Client) HostAcceptingContractsPost(acceptingContracts bool) (err error) {
	values := url.Values{}
	values.Set("acceptingcontracts", strconv.FormatBool(acceptingContracts))
	err = c.post("/host", values.Encode(), nil)
	return
}

// HostStorageFoldersAddPost uses the /host/storage/folders/add api endpoint to
// add a storage folder to a host
func (c *Client) HostStorageFoldersAddPost(path string, size uint64) (err error) {
	values := url.Values{}
	values.Set("path", path)
	values.Set("size", strconv.FormatUint(size, 10))
	err = c.post("/host/storage/folders/add", values.Encode(), nil)
	return
}

// HostGet requests the /host endpoint.
func (c *Client) HostGet() (hg api.HostGET, err error) {
	err = c.get("/host", &hg)
	return
}
