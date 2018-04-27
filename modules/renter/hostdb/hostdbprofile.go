package hostdb

import (
	"errors"
	"github.com/pachisi456/sia-hostdb-profiles/modules"
)

var (
	errHostdbProfileExists = errors.New("hostdb profile with provided name already exists")
	errNoSuchStorageTier   = errors.New("no such storage tier, see `siac hostdb profiles add " +
		"-h` for possible storage tiers")
)

// storagetiers is an array of all possible storage tiers that the user can
// choose between when creating a new hostdb profile. Depending on the
// "temperature" set here less performant but cheaper hosts (cold) or more
// performant but more expensive hosts (hot) are picked in the host selection.
// Warm is balanced between hot and cold and thus the default setting.
var storagetiers = []string{"cold", "warm", "hot"}

// NewHostDBProfile returns a new hostdb profile with the given parameters.
func NewHostDBProfile(name string, storagetier string) modules.HostDBProfile {
	return modules.HostDBProfile{
		Name:        name,
		Storagetier: storagetier,
		Location:    nil,
	}
}

// AddHostDBProfiles adds a new hostdb profile to HostDBProfiles.
func (hdb *HostDB) AddHostDBProfiles(name string, storagetier string) (err error) {
	hdbp := hdb.HostDBProfiles()
	hdbp.Mu.Lock()
	defer hdbp.Mu.Unlock()

	// check if hostdb profile with that name already exists
	if exists, _ := hdb.ProfileExists(name); exists {
		return errHostdbProfileExists
	}
	// check if provided storage tier is valid
	validTier := false
	for _, v := range storagetiers {
		if v == storagetier {
			validTier = true
			break
		}
	}
	if !validTier {
		return errNoSuchStorageTier
	}

	hdbp.Profiles = append(hdbp.Profiles, modules.HostDBProfile{
		Name:        name,
		Storagetier: storagetier,
		Location:    nil,
	})

	// write data back to the hostdb and save it
	hdb.hostdbProfiles.Profiles = hdbp.Profiles
	hdb.mu.Lock()
	err = hdb.saveSync()
	hdb.mu.Unlock()
	if err != nil {
		hdb.log.Println("Unable to save the hostdb profile:", err)
	}
	return
}

// HostDBProfiles returns the array of all set hostdb profiles.
func (hdb *HostDB) HostDBProfiles() modules.HostDBProfiles { return hdb.hostdbProfiles }

// ProfileExists checks whether a hostdb profile with the given `name` exists.
// Returns true and the index the profile is located at in the HostDBProfiles.profiles
// array if it exists, otherwise false and 0.
func (hdb *HostDB) ProfileExists(name string) (exists bool, index int) {
	hdb.hostdbProfiles.Mu.Lock()
	defer hdb.hostdbProfiles.Mu.Unlock()
	for i, v := range hdb.hostdbProfiles.Profiles {
		if name == v.Name {
			exists = true
			index = i
			return
		}
	}
	return
}
