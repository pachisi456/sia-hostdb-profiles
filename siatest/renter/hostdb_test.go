package renter

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/node"
	"gitlab.com/NebulousLabs/Sia/node/api/client"
	"gitlab.com/NebulousLabs/Sia/siatest"
	"gitlab.com/NebulousLabs/errors"
)

// TestInitialScanComplete tests if the initialScanComplete field is set
// correctly.
func TestInitialScanComplete(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Get a directory for testing.
	testDir := renterTestDir(t.Name())

	// Create a group. The renter should block the scanning thread using a
	// dependency.
	deps := &dependencyBlockScan{}
	renterTemplate := node.Renter(filepath.Join(testDir, "renter"))
	renterTemplate.SkipSetAllowance = true
	renterTemplate.SkipHostDiscovery = true
	renterTemplate.HostDBDeps = deps

	tg, err := siatest.NewGroup(testDir, renterTemplate, node.Host(filepath.Join(testDir, "host")),
		siatest.Miner(filepath.Join(testDir, "miner")))
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		deps.Scan()
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// The renter should have 1 offline host in its database and
	// initialScanComplete should be false.
	renter := tg.Renters()[0]
	hdag, err := renter.HostDbAllGet()
	if err != nil {
		t.Fatal(err)
	}
	hdg, err := renter.HostDbGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(hdag.Hosts) != 1 {
		t.Fatalf("HostDB should have 1 host but had %v", len(hdag.Hosts))
	}
	if hdag.Hosts[0].ScanHistory.Len() > 0 {
		t.Fatalf("Host should have 0 scans but had %v", hdag.Hosts[0].ScanHistory.Len())
	}
	if hdg.InitialScanComplete {
		t.Fatal("Initial scan is complete even though it shouldn't")
	}

	deps.Scan()
	err = build.Retry(600, 100*time.Millisecond, func() error {
		hdag, err := renter.HostDbAllGet()
		if err != nil {
			t.Fatal(err)
		}
		hdg, err := renter.HostDbGet()
		if err != nil {
			t.Fatal(err)
		}
		if !hdg.InitialScanComplete {
			return fmt.Errorf("Initial scan is not complete even though it should be")
		}
		if len(hdag.Hosts) != 1 {
			return fmt.Errorf("HostDB should have 1 host but had %v", len(hdag.Hosts))
		}
		if hdag.Hosts[0].ScanHistory.Len() == 0 {
			return fmt.Errorf("Host should have >0 scans but had %v", hdag.Hosts[0].ScanHistory.Len())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPruneRedundantAddressRange checks if the contractor correctly cancels
// contracts with redundant IP ranges.
func TestPruneRedundantAddressRange(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get the testDir for this test.
	testDir := renterTestDir(t.Name())

	// Create a group with a few hosts.
	groupParams := siatest.GroupParams{
		Hosts:  3,
		Miners: 1,
	}
	tg, err := siatest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Get the ports of the hosts.
	allHosts := tg.Hosts()
	hg1, err1 := allHosts[0].HostGet()
	hg2, err2 := allHosts[1].HostGet()
	hg3, err3 := allHosts[2].HostGet()
	err = errors.Compose(err1, err2, err3)
	if err != nil {
		t.Fatal("Failed to get ports from at least one host", err)
	}
	host1Port := hg1.ExternalSettings.NetAddress.Port()
	host2Port := hg2.ExternalSettings.NetAddress.Port()
	host3Port := hg3.ExternalSettings.NetAddress.Port()

	// Reannounce the hosts with custom hostnames which match the hostnames
	// from the custom resolver method. We announce host1 first and host3 last
	// to make sure host1 is the 'oldest' and host3 the 'youngest'.
	err1 = allHosts[0].HostAnnounceAddrPost(modules.NetAddress(fmt.Sprintf("host1.com:%s", host1Port)))
	err2 = tg.Miners()[0].MineBlock()
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal("failed to announce host1")
	}
	err1 = allHosts[1].HostAnnounceAddrPost(modules.NetAddress(fmt.Sprintf("host2.com:%s", host2Port)))
	err2 = tg.Miners()[0].MineBlock()
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal("failed to announce host2")
	}
	err1 = allHosts[2].HostAnnounceAddrPost(modules.NetAddress(fmt.Sprintf("host3.com:%s", host3Port)))
	err2 = tg.Miners()[0].MineBlock()
	if err := errors.Compose(err1, err2); err != nil {
		t.Fatal("failed to announce host3")
	}

	// Add a renter with a custom resolver to the group.
	renterTemplate := node.Renter(testDir + "/renter")
	renterTemplate.HostDBDeps = siatest.NewDependencyCustomResolver(func(host string) ([]net.IP, error) {
		switch host {
		case "host1.com":
			return []net.IP{{128, 0, 0, 1}}, nil
		case "host2.com":
			return []net.IP{{129, 0, 0, 1}}, nil
		case "host3.com":
			return []net.IP{{130, 0, 0, 1}}, nil
		case "host4.com":
			return []net.IP{{130, 0, 0, 2}}, nil
		case "localhost":
			return []net.IP{{127, 0, 0, 1}}, nil
		default:
			panic("shouldn't happen")
		}
	})
	renterTemplate.ContractorDeps = renterTemplate.HostDBDeps

	// Adding a custom RenewWindow will make a contract renewal during the test
	// unlikely.
	renterTemplate.Allowance = siatest.DefaultAllowance
	renterTemplate.Allowance.Period *= 2
	renterTemplate.Allowance.RenewWindow = 1
	renterTemplate.Allowance.Hosts = uint64(len(allHosts))
	_, err = tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// We expect the renter to have 3 active contracts.
	renter := tg.Renters()[0]
	contracts, err := renter.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts.ActiveContracts) != len(allHosts) {
		t.Fatalf("Expected %v active contracts but got %v", len(allHosts), len(contracts.ActiveContracts))
	}

	// Reannounce host1 as host4 which creates a violation with host3 and
	// causes host1 to be the 'youngest'.
	err = allHosts[0].HostAnnounceAddrPost(modules.NetAddress(fmt.Sprintf("host4.com:%s", host1Port)))
	if err != nil {
		t.Fatal("Failed to reannonce host 1")
	}

	// Mine the announcement.
	if err := tg.Miners()[0].MineBlock(); err != nil {
		t.Fatal("Failed to mine block", err)
	}

	// Check that all the hosts have been scanned.
	hdag, err := renter.HostDbAllGet()
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range hdag.Hosts {
		if host.LastIPNetChange.IsZero() {
			t.Fatal("host's LastIPNetChange is still zero", host.NetAddress.Host())
		}
		if len(host.IPNets) == 0 {
			t.Fatal("host doesn't have any IPNets associated with it")
		}
	}

	retry := 0
	err = build.Retry(100, 100*time.Millisecond, func() error {
		// Mine new blocks periodically.
		if retry%25 == 0 {
			if tg.Miners()[0].MineBlock() != nil {
				return err
			}
		}
		retry++
		// The renter should now have 2 active contracts and 1 inactive one.
		// The inactive one should be host4 since it's the 'youngest'.
		contracts, err = renter.RenterInactiveContractsGet()
		if err != nil {
			return err
		}
		if len(contracts.ActiveContracts) != len(allHosts)-1 {
			return fmt.Errorf("Expected %v active contracts but got %v", len(allHosts)-1, len(contracts.ActiveContracts))
		}
		if len(contracts.InactiveContracts) != 1 {
			return fmt.Errorf("Expected 1 inactive contract but got %v", len(contracts.InactiveContracts))
		}
		canceledHost := contracts.InactiveContracts[0].NetAddress.Host()
		if canceledHost != "host4.com" {
			return fmt.Errorf("Expected canceled contract to be host4.com but was %v", canceledHost)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestSelectRandomCanceledHost makes sure that we can form a contract with a
// hostB even if it has a conflict with a hostA iff hostA is canceled.
func TestSelectRandomCanceledHost(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// Get the testDir for this test.
	testDir := renterTestDir(t.Name())

	// Create a group with a single host.
	groupParams := siatest.GroupParams{
		Hosts:  1,
		Miners: 1,
	}
	tg, err := siatest.NewGroupFromTemplate(testDir, groupParams)
	if err != nil {
		t.Fatal("Failed to create group: ", err)
	}
	defer func() {
		if err := tg.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	// Get the host's port.
	hg, err := tg.Hosts()[0].HostGet()
	if err != nil {
		t.Fatal("Failed to get port from host", err)
	}
	hostPort := hg.ExternalSettings.NetAddress.Port()

	// Reannounce the hosts with custom hostnames which match the hostnames from the custom resolver method.
	err = tg.Hosts()[0].HostAnnounceAddrPost(modules.NetAddress(fmt.Sprintf("host1.com:%s", hostPort)))
	if err != nil {
		t.Fatal("Failed to reannounce at least one of the hosts", err)
	}

	// Mine the announcements.
	if err := tg.Miners()[0].MineBlock(); err != nil {
		t.Fatal("Failed to mine block", err)
	}

	// Add a renter with a custom resolver to the group.
	renterTemplate := node.Renter(testDir + "/renter")
	renterTemplate.HostDBDeps = siatest.NewDependencyCustomResolver(func(host string) ([]net.IP, error) {
		switch host {
		case "host1.com":
			return []net.IP{{128, 0, 0, 1}}, nil
		case "host2.com":
			return []net.IP{{128, 1, 0, 1}}, nil
		case "localhost":
			return []net.IP{{127, 0, 0, 1}}, nil
		default:
			panic("shouldn't happen")
		}
	})
	renterTemplate.ContractorDeps = renterTemplate.HostDBDeps

	// Create renter.
	_, err = tg.AddNodes(renterTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// We expect the renter to have 1 active contract.
	renter := tg.Renters()[0]
	contracts, err := renter.RenterContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts.ActiveContracts) != 1 {
		t.Fatalf("Expected 1 active contract but got %v", len(contracts.Contracts))
	}

	// Cancel the active contract.
	err = renter.RenterContractCancelPost(contracts.ActiveContracts[0].ID)
	if err != nil {
		t.Fatal("Failed to cancel contract", err)
	}

	// We expect the renter to have 1 inactive contract.
	contracts, err = renter.RenterInactiveContractsGet()
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts.InactiveContracts) != 1 {
		t.Fatalf("Expected 1 inactive contract but got %v", len(contracts.InactiveContracts))
	}

	// Create a new host which doesn't announce itself right away.
	newHostTemplate := node.Host(testDir + "/host")
	newHostTemplate.SkipHostAnnouncement = true
	newHost, err := tg.AddNodes(newHostTemplate)
	if err != nil {
		t.Fatal(err)
	}

	// Announce the new host as host2.com. That should cause a conflict between
	// the hosts. That shouldn't be an issue though since one of the hosts has
	// a canceled contract.
	hg, err = newHost[0].HostGet()
	if err != nil {
		t.Fatal("Failed to get port from host", err)
	}
	hostPort = hg.ExternalSettings.NetAddress.Port()
	err1 := newHost[0].HostModifySettingPost(client.HostParamAcceptingContracts, true)
	err2 := newHost[0].HostAnnounceAddrPost(modules.NetAddress(fmt.Sprintf("host2.com:%s", hostPort)))
	err = errors.Compose(err1, err2)
	if err != nil {
		t.Fatal("Failed to announce the new host", err)
	}

	// Mine the announcement.
	if err := tg.Miners()[0].MineBlock(); err != nil {
		t.Fatal("Failed to mine block", err)
	}

	// The renter should have an active contract with the new host and an
	// inactive contract with the old host now.
	numRetries := 0
	err = build.Retry(100, 100*time.Millisecond, func() error {
		if numRetries%10 == 0 {
			err := tg.Miners()[0].MineBlock()
			if err != nil {
				return err
			}
		}
		numRetries++
		// Get the active and inactive contracts.
		contracts, err := renter.RenterInactiveContractsGet()
		if err != nil {
			return err
		}
		// Should have 1 active contract and 1 inactive contract.
		if len(contracts.ActiveContracts) != 1 || len(contracts.InactiveContracts) != 1 {
			return fmt.Errorf("Expected 1 active contract and 1 inactive contract. (%v/%v)",
				len(contracts.ActiveContracts), len(contracts.InactiveContracts))
		}
		// The active contract should be with host2.
		if contracts.ActiveContracts[0].NetAddress.Host() != "host2.com" {
			return fmt.Errorf("active contract should be with host2.com")
		}
		// The inactive contract should be with host1.
		if contracts.InactiveContracts[0].NetAddress.Host() != "host1.com" {
			return fmt.Errorf("active contract should be with host1.com")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
