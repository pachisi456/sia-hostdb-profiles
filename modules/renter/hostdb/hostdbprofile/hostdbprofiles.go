package hostdbprofile

import (
	"errors"
	"sync"
)

var (
	errHostdbProfileExists = errors.New("hostdb profile with provided name already exists")
	errLocationNotSet      = errors.New("provided location cannot be removed as it is not set")
	errLocationAlreadySet  = errors.New("provided location is already set")
	errNoSuchHostdbProfile = errors.New("hostdb profile with provided name does not exist")
	errNoSuchLocation      = errors.New("provided location not recognized")
	errNoSuchSetting       = errors.New("provided setting not recognized")
	errNoSuchStorageTier   = errors.New("no such storage tier, see `siac hostdb profiles add " +
		"-h` for possible storage tiers")
	errStoragetierAlreadySet = errors.New("provided storage tier is already set")
)

// HostDBProfiles is the collection of all hostdb profiles the renter created to
// customize the host selection.
type HostDBProfiles struct {
	profiles []HostDBProfile
	mu       sync.Mutex
}

func NewHostDBProfiles(name string, storagetier string) HostDBProfiles {
	return HostDBProfiles{
		profiles: []HostDBProfile{{
			Name:        "default",
			Storagetier: "warm",
			Location:    nil,
		},
		},
	}
}

// AddHostDBProfile adds a new hostdb profile to HostDBProfiles.
func (hdbp *HostDBProfiles) AddHostDBProfile(name, storagetier string) (err error) {
	hdbp.mu.Lock()
	defer hdbp.mu.Unlock()

	// check if hostdb profile with that name already exists
	if exists, _ := hdbp.profileExists(name); exists {
		return errHostdbProfileExists
	}

	// check if provided storage tier is valid
	if valid := storagetierValid(storagetier); !valid {
		return errNoSuchStorageTier
	}

	// add new hostdb profile
	hdbp.profiles = append(hdbp.profiles, HostDBProfile{
		Name:        name,
		Storagetier: storagetier,
		Location:    nil,
	})
	return
}

// ConfigHostDBProfiles updates the provided setting of the hostdb profile with the provided
// name to the provided value.
func (hdbp *HostDBProfiles) ConfigHostDBProfiles(name, setting, value string) (err error) {
	hdbp.mu.Lock()
	defer hdbp.mu.Unlock()

	// check if profile exists
	exists, i := hdbp.profileExists(name)
	if !exists {
		return errNoSuchHostdbProfile
	}

	return hdbp.profiles[i].configHostDBProfile(setting, value)
}

// HostDBProfiles returns the array of set hostdb profiles.
func (hdbp *HostDBProfiles) HostDBProfiles() []HostDBProfile {
	hdbp.mu.Lock()
	defer hdbp.mu.Unlock()
	return hdbp.profiles
}

// profileExists checks whether a hostdb profile with the given `name` exists.
// Returns true and the index the profile is located at in the HostDBProfiles.profiles
// array if it exists, otherwise false and 0.
func (hdbp *HostDBProfiles) profileExists(name string) (exists bool, index int) {
	for i, v := range hdbp.profiles {
		if name == v.Name {
			exists = true
			index = i
			return
		}
	}
	return
}

// SetHostDBProfiles sets the hostdb profiles to the profiles passed to the function (from persist data)
func (hdbp *HostDBProfiles) SetHostDBProfiles(profiles []HostDBProfile) {
	hdbp.profiles = profiles
	return
}

// storageTierValid is a helper function that returns true if the provided storage tier is valid,
// otherwise false.
func storagetierValid(storagetier string) (valid bool) {
	for _, v := range storagetiers {
		if v == storagetier {
			return true
		}
	}
	return
}

// locationValid is helper function that returns true if the provided location is valid,
// otherwise false.
func locationValid(location string) (valid bool) {
	for _, v := range locations {
		if v == location {
			return true
		}
	}
	return
}
