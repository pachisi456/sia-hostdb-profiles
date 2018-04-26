package api

import (
	"fmt"
	"net/http"

	"github.com/pachisi456/sia-hostdb-profiles/modules"
	"github.com/pachisi456/sia-hostdb-profiles/types"

	"github.com/julienschmidt/httprouter"
)

type (
	// ExtendedHostDBEntry is an extension to modules.HostDBEntry that includes
	// the string representation of the public key, otherwise presented as two
	// fields, a string and a base64 encoded byte slice.
	ExtendedHostDBEntry struct {
		modules.HostDBEntry
		PublicKeyString string `json:"publickeystring"`
	}

	// HostdbActiveGET lists active hosts on the network.
	HostdbActiveGET struct {
		Hosts []ExtendedHostDBEntry `json:"hosts"`
	}

	// HostdbAllGET lists all hosts that the renter is aware of.
	HostdbAllGET struct {
		Hosts []ExtendedHostDBEntry `json:"hosts"`
	}

	// HostdbHostsGET lists detailed statistics for a particular host, selected
	// by pubkey.
	HostdbHostsGET struct {
		Entry          ExtendedHostDBEntry        `json:"entry"`
		ScoreBreakdown modules.HostScoreBreakdown `json:"scorebreakdown"`
	}
)

// hostdbActiveHandler handles the API call asking for the list of active
// hosts.
func (api *API) hostdbActiveHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var numHosts uint64
	hosts := api.renter.ActiveHosts()

	if req.FormValue("numhosts") == "" {
		// Default value for 'numhosts' is all of them.
		numHosts = uint64(len(hosts))
	} else {
		// Parse the value for 'numhosts'.
		_, err := fmt.Sscan(req.FormValue("numhosts"), &numHosts)
		if err != nil {
			WriteError(w, Error{err.Error()}, http.StatusBadRequest)
			return
		}

		// Catch any boundary errors.
		if numHosts > uint64(len(hosts)) {
			numHosts = uint64(len(hosts))
		}
	}

	// Convert the entries into extended entries.
	var extendedHosts []ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
		})
	}

	WriteJSON(w, HostdbActiveGET{
		Hosts: extendedHosts[:numHosts],
	})
}

// hostdbAllHandler handles the API call asking for the list of all hosts.
func (api *API) hostdbAllHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the set of all hosts and convert them into extended hosts.
	hosts := api.renter.AllHosts()
	var extendedHosts []ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
		})
	}

	WriteJSON(w, HostdbAllGET{
		Hosts: extendedHosts,
	})
}

// hostdbHostsHandler handles the API call asking for a specific host,
// returning detailed informatino about that host.
func (api *API) hostdbHostsHandler(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	var pk types.SiaPublicKey
	pk.LoadString(ps.ByName("pubkey"))

	entry, exists := api.renter.Host(pk)
	if !exists {
		WriteError(w, Error{"requested host does not exist"}, http.StatusBadRequest)
		return
	}
	breakdown := api.renter.ScoreBreakdown(entry)

	// Extend the hostdb entry  to have the public key string.
	extendedEntry := ExtendedHostDBEntry{
		HostDBEntry:     entry,
		PublicKeyString: entry.PublicKey.String(),
	}
	WriteJSON(w, HostdbHostsGET{
		Entry:          extendedEntry,
		ScoreBreakdown: breakdown,
	})
}

// hostDBProfilesHandlerGET handles the API call asking for the list of hostdb profiles and returns such.
func (api *API) hostDBProfilesHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	hdbprofiles := api.renter.HostDBProfiles()
	WriteJSON(w, hdbprofiles.Profiles)
}

// hostDBProfilesAddHandler handles the API call for adding a new hostdb profile
func (api *API) hostDBProfilesAddHandler(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	name := req.FormValue("name")
	storagetier := req.FormValue("storagetier")
	err := api.renter.AddHostDBProfiles(name, storagetier)
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}
