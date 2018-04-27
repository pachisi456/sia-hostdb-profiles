package hostdb

import (
	"errors"
	"github.com/pachisi456/sia-hostdb-profiles/modules"
)

var (
	errHostdbProfileExists = errors.New("hostdb profile with provided name already exists")
	errLocationNotSet      = errors.New("provided location cannot be removed as it is not set")
	errNoSuchHostdbProfile = errors.New("hostdb profile with provided name does not exist")
	errNoSuchLocation      = errors.New("provided location not recognized")
	errNoSuchSetting       = errors.New("provided setting not recognized")
	errNoSuchStorageTier   = errors.New("no such storage tier, see `siac hostdb profiles add " +
		"-h` for possible storage tiers")
)

var (
	// storagetiers is an array of all possible storage tiers that the user can
	// choose between when creating a new hostdb profile. Depending on the
	// "temperature" set here less performant but cheaper hosts (cold) or more
	// performant but more expensive hosts (hot) are picked in the host selection.
	// Warm is balanced between hot and cold and thus the default setting.
	storagetiers = []string{"cold", "warm", "hot"}

	// locations is the list of possible locations the user can restrict their hostdb
	// profile to. Siad will then only form contracts with hosts in those locations
	// (according to ip address)
	locations = []string{"germany", "austria", "switzerland"}
)

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
	if valid := storagetierValid(storagetier); !valid {
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

// ConfigHostDBProfiles updates the provided setting of the hostdb profile with the provided
// name to the provided value. All parameters are checked for validity.
func (hdb *HostDB) ConfigHostDBProfiles(name, setting, value string) (err error) {
	// check if provided hostdb profile exists
	exists, i := hdb.ProfileExists(name)
	if !exists {
		return errNoSuchHostdbProfile
	}

	hdb.hostdbProfiles.Mu.Lock()
	hdbp := &hdb.hostdbProfiles.Profiles[i]

	switch setting {
	case "storagetier":
		// check if provided storage tier is valid
		if valid := storagetierValid(value); !valid {
			hdb.hostdbProfiles.Mu.Unlock()
			return errNoSuchStorageTier
		}
		// adjust the storage tier
		hdbp.Storagetier = value
	case "addlocation":
		// check if provided location is valid
		if valid := locationValid(value); !valid {
			hdb.hostdbProfiles.Mu.Unlock()
			return errNoSuchLocation
		}
		// add location
		hdbp.Location = append(hdbp.Location, value)
	case "removelocation":
		// check if and at what index the provided location is set
		index := -1
		for i, location := range hdbp.Location {
			if location == value {
				index = i
				break
			}
		}

		// return error if location not found
		if index < 0 {
			hdb.hostdbProfiles.Mu.Unlock()
			return errLocationNotSet
		}

		// delete the location
		hdbp.Location = append(hdbp.Location[:index], hdbp.Location[index+1:]...)
	default:
		hdb.hostdbProfiles.Mu.Unlock()
		return errNoSuchSetting
	}

	hdb.hostdbProfiles.Mu.Unlock()

	// save changes
	hdb.mu.Lock()
	hdb.saveSync()
	hdb.mu.Unlock()
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

// storageTierValid will return true if the provided storage tier is valid, otherwise
// false.
func storagetierValid(storagetier string) (valid bool) {
	for _, v := range storagetiers {
		if v == storagetier {
			return true
		}
	}
	return
}

// locationValid returns true if the provided location is valid, otherwise false.
func locationValid(location string) (valid bool) {
	for _, v := range locations {
		if v == location {
			return true
		}
	}
	return
}
