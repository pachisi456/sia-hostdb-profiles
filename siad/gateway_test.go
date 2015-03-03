package main

import (
	"testing"

	"github.com/NebulousLabs/Sia/modules"
)

// addPeer creates a new daemonTester and bootstraps it to dt. It returns the
// new peer.
func (dt *daemonTester) addPeer() *daemonTester {
	// Create a new peer and bootstrap it to dt.
	newPeer := newDaemonTester(dt.T)
	err := newPeer.gateway.Bootstrap(dt.netAddress())
	if err != nil {
		dt.Fatal("bootstrap failed:", err)
	}

	// Wait for RPC to finish, then check that each has the same number of
	// peers.
	<-dt.rpcChan
	if len(dt.gateway.Info().Peers) != len(newPeer.gateway.Info().Peers) {
		dt.Fatal("bootstrap did not synchronize peer lists")
	}
	return newPeer
}

// TestPeering tests that peers are properly announced and relayed throughout
// the network.
func TestPeering(t *testing.T) {
	// Create to peers and add the first to the second.
	peer1 := newDaemonTester(t)
	peer2 := newDaemonTester(t)
	peer1.callAPI("/gateway/add?addr=" + string(peer2.netAddress()))

	// Check that the first has the second as a peer.
	var info modules.GatewayInfo
	peer1.getAPI("/gateway/status", &info)
	if len(info.Peers) != 1 || info.Peers[0] != peer2.netAddress() {
		t.Fatal("/gateway/add did not add peer", peer2.netAddress())
	}

	// Create a third peer that bootstraps to the first peer and check that it
	// reports the others as peers.
	peer3 := peer1.addPeer()
	peer3.getAPI("/gateway/status", &info)
	if len(info.Peers) != 2 {
		t.Fatal("bootstrap peer did not share its peers")
	}

	// peer2 should have received peer3 via peer1. Note that it does not have
	// peer1 though, because /gateway/add does not contact the added peer.
	peer2.getAPI("/gateway/status", &info)
	if len(info.Peers) != 1 || info.Peers[0] != peer3.netAddress() {
		t.Fatal("bootstrap peer did not relay the bootstrapping peer")
	}
}

// TestTransactionRelay checks that an unconfirmed transaction is relayed to
// all peers.
func TestTransactionRelay(t *testing.T) {
	// Create a daemon tester and give it a peer.
	dt := newDaemonTester(t)
	dt2 := dt.addPeer()

	// Give some money to the daemon tester.
	dt.mineMoney()

	// Make sure both daemons have empty transaction pools.
	tset, err := dt.tpool.TransactionSet()
	if err != nil {
		t.Fatal(err)
	}
	tset2, err := dt2.tpool.TransactionSet()
	if err != nil {
		t.Fatal(err)
	}
	if len(tset) != 0 || len(tset2) != 0 {
		t.Fatal("transaction set is not empty after creating new daemon tester")
	}

	// Get the original balances of each daemon for later comparison.
	origBal := dt.wallet.Balance(false)
	origBal2 := dt2.wallet.Balance(false)

	// Create a transaction in the first daemon and check that it propagates to
	// the second. The check is done via spinning because network propagation
	// will take an unknown amount of time.
	dt.callAPI("/wallet/send?amount=15&dest=" + dt2.coinAddress())
	for len(tset) != 0 || len(tset2) != 0 {
		tset, err = dt.tpool.TransactionSet()
		if err != nil {
			t.Fatal(err)
		}
		tset2, err = dt2.tpool.TransactionSet()
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(time.Millisecond)
	}

	// Check that the balances of each have updated appropriately, in
	// accordance with 0-conf.
	if origBal.Sub(consensus.NewCurrency64(15)).Cmp(dt.wallet.Balance(false)) != 0 {
		t.Error(origBal.Big())
		t.Error(dt.wallet.Balance(false).Big())
		t.Error("balances are incorrect for 0-conf transaction")
	}
	if origBal2.Add(consensus.NewCurrency64(15)).Cmp(dt2.wallet.Balance(false)) != 0 {
		t.Error(origBal2.Big())
		t.Error(dt2.wallet.Balance(false).Big())
		t.Error("balances are incorrect for 0-conf transaction")
	}
}
