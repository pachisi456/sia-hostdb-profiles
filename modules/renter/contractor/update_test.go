package contractor

import (
	"errors"
	"testing"
	"time"

	"github.com/pachisi456/sia-hostdb-profiles/build"
	"github.com/pachisi456/sia-hostdb-profiles/crypto"
	"github.com/pachisi456/sia-hostdb-profiles/modules"
	"github.com/pachisi456/sia-hostdb-profiles/types"
	"github.com/NebulousLabs/fastrand"
)

// TestIntegrationAutoRenew tests that contracts are automatically renwed at
// the expected block height.
func TestIntegrationAutoRenew(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	// create testing trio
	_, c, m, err := newTestingTrio(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// form a contract with the host
	a := modules.Allowance{
		Funds:       types.SiacoinPrecision.Mul64(100), // 100 SC
		Hosts:       1,
		Period:      50,
		RenewWindow: 10,
	}
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) == 0 {
			return errors.New("contracts were not formed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	contract := c.Contracts()[0]

	// revise the contract
	editor, err := c.Editor(contract.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := fastrand.Bytes(int(modules.SectorSize))
	// insert the sector
	_, err = editor.Upload(data)
	if err != nil {
		t.Fatal(err)
	}
	err = editor.Close()
	if err != nil {
		t.Fatal(err)
	}

	// mine until we enter the renew window
	renewHeight := contract.EndHeight - c.allowance.RenewWindow
	for c.blockHeight < renewHeight {
		_, err := m.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	// wait for goroutine in ProcessConsensusChange to finish
	time.Sleep(100 * time.Millisecond)
	c.maintenanceLock.Lock()
	c.maintenanceLock.Unlock()

	// check renewed contract
	contract = c.Contracts()[0]
	if contract.EndHeight != c.blockHeight+c.allowance.Period {
		t.Fatal("wrong window start:", contract.EndHeight)
	}
}

// TestIntegrationRenewInvalidate tests that editors and downloaders are
// properly invalidated when a renew is queued.
func TestIntegrationRenewInvalidate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	// create testing trio
	_, c, m, err := newTestingTrio(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// form a contract with the host
	a := modules.Allowance{
		Funds:       types.SiacoinPrecision.Mul64(100), // 100 SC
		Hosts:       1,
		Period:      50,
		RenewWindow: 10,
	}
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) == 0 {
			return errors.New("contracts were not formed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	contract := c.Contracts()[0]

	// revise the contract
	editor, err := c.Editor(contract.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := fastrand.Bytes(int(modules.SectorSize))
	// insert the sector
	_, err = editor.Upload(data)
	if err != nil {
		t.Fatal(err)
	}

	// mine until we enter the renew window; the editor should be invalidated
	renewHeight := contract.EndHeight - c.allowance.RenewWindow
	for c.blockHeight < renewHeight {
		_, err := m.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	// wait for goroutine in ProcessConsensusChange to finish
	time.Sleep(100 * time.Millisecond)
	c.maintenanceLock.Lock()
	c.maintenanceLock.Unlock()

	// check renewed contract
	contract = c.Contracts()[0]
	c.mu.Lock()
	if contract.EndHeight != c.blockHeight+c.allowance.Period {
		t.Error("wrong window start:", contract.EndHeight)
	}
	c.mu.Unlock()

	// editor should have been invalidated
	_, err = editor.Upload(make([]byte, modules.SectorSize))
	if err != errInvalidEditor {
		t.Error("expected invalid editor error; got", err)
	}
	editor.Close()

	// create a downloader
	downloader, err := c.Downloader(contract.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	// mine until we enter the renew window
	renewHeight = contract.EndHeight - c.allowance.RenewWindow
	for c.blockHeight < renewHeight {
		_, err := m.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	// wait for goroutine in ProcessConsensusChange to finish
	time.Sleep(100 * time.Millisecond)
	c.maintenanceLock.Lock()
	c.maintenanceLock.Unlock()

	// downloader should have been invalidated
	_, err = downloader.Sector(crypto.Hash{})
	if err != errInvalidDownloader {
		t.Error("expected invalid downloader error; got", err)
	}
	downloader.Close()
}
