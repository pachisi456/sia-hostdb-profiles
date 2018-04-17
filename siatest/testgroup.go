package siatest

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/NebulousLabs/Sia/build"
	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/node"
	"github.com/NebulousLabs/Sia/node/api/client"
	"github.com/NebulousLabs/Sia/types"
	"github.com/NebulousLabs/errors"
	"github.com/NebulousLabs/fastrand"
)

type (
	// GroupParams is a helper struct to make creating TestGroups easier.
	GroupParams struct {
		Hosts   int // number of hosts to create
		Renters int // number of renters to create
		Miners  int // number of miners to create
	}

	// TestGroup is a group of of TestNodes that are funded, synced and ready
	// for upload, download and mining depending on their configuration
	TestGroup struct {
		nodes   map[*TestNode]struct{}
		hosts   map[*TestNode]struct{}
		renters map[*TestNode]struct{}
		miners  map[*TestNode]struct{}

		dir string
	}
)

var (
	// defaultAllowance is the allowance used for the group's renters
	defaultAllowance = modules.Allowance{
		Funds:       types.SiacoinPrecision.Mul64(1e3),
		Hosts:       5,
		Period:      50,
		RenewWindow: 10,
	}
)

// NewGroup creates a group of TestNodes from node params. All the nodes will
// be connected, synced and funded. Hosts nodes are also announced.
func NewGroup(nodeParams ...node.NodeParams) (*TestGroup, error) {
	// Create and init group
	tg := &TestGroup{
		nodes:   make(map[*TestNode]struct{}),
		hosts:   make(map[*TestNode]struct{}),
		renters: make(map[*TestNode]struct{}),
		miners:  make(map[*TestNode]struct{}),
	}

	// Create node and add it to the correct groups
	nodes := make([]*TestNode, 0, len(nodeParams))
	for _, np := range nodeParams {
		node, err := NewCleanNode(np)
		if err != nil {
			return nil, errors.AddContext(err, "failed to create clean node")
		}
		// Add node to nodes
		tg.nodes[node] = struct{}{}
		nodes = append(nodes, node)
		// Add node to hosts
		if np.Host != nil || np.CreateHost {
			tg.hosts[node] = struct{}{}
		}
		// Add node to renters
		if np.Renter != nil || np.CreateRenter {
			tg.renters[node] = struct{}{}
		}
		// Add node to miners
		if np.Miner != nil || np.CreateMiner {
			tg.miners[node] = struct{}{}
		}
	}

	// Fully connect nodes
	if err := fullyConnectNodes(nodes); err != nil {
		return nil, errors.AddContext(err, "failed to fully connect nodes")
	}
	// Get a miner and mine some blocks to generate coins
	if len(tg.miners) == 0 {
		return nil, errors.New("cannot fund group without miners")
	}
	miner := tg.Miners()[0]
	for i := types.BlockHeight(0); i <= types.MaturityDelay; i++ {
		if err := miner.MineBlock(); err != nil {
			return nil, errors.AddContext(err, "failed to mine block for funding")
		}
	}
	// Fund nodes
	if err := fundNodes(miner, tg.nodes); err != nil {
		return nil, errors.AddContext(err, "failed to fund nodes")
	}
	// Add storage to hosts
	if err := addStorageFolderToHosts(tg.hosts); err != nil {
		return nil, errors.AddContext(err, "failed to add storage to nodes")
	}
	// Announce hosts
	if err := announceHosts(tg.hosts); err != nil {
		return nil, errors.AddContext(err, "failed to announce hosts")
	}
	// Mine a block to get the announcements confirmed
	if err := miner.MineBlock(); err != nil {
		return nil, errors.AddContext(err, "failed to mine host announcements")
	}
	// Block until all hosts show up as active in the renters' hostdbs
	if err := hostsInRenterDBCheck(miner, tg.renters, tg.hosts); err != nil {
		return nil, build.ExtendErr("renter database check failed", err)
	}
	// Set renter allowances
	if err := setRenterAllowances(tg.renters); err != nil {
		return nil, errors.AddContext(err, "failed to set renter allowance")
	}
	// Wait for all the renters to form contracts
	if err := waitForContracts(miner, tg.renters, tg.hosts); err != nil {
		return nil, errors.AddContext(err, "renters failed to form contracts")
	}
	// Make sure all nodes are synced
	if err := synchronizationCheck(miner, tg.nodes); err != nil {
		return nil, errors.AddContext(err, "synchronization check failed")
	}
	return tg, nil
}

// NewGroupFromTemplate will create hosts, renters and miners according to the
// settings in groupParams.
func NewGroupFromTemplate(groupParams GroupParams) (*TestGroup, error) {
	var params []node.NodeParams
	// Create host params
	for i := 0; i < groupParams.Hosts; i++ {
		params = append(params, node.Host(randomDir()))
	}
	// Create renter params
	for i := 0; i < groupParams.Renters; i++ {
		params = append(params, node.Renter(randomDir()))
	}
	// Create miner params
	for i := 0; i < groupParams.Miners; i++ {
		params = append(params, Miner(randomDir()))
	}
	return NewGroup(params...)
}

// addStorageFolderToHosts adds a single storage folder to each host.
func addStorageFolderToHosts(hosts map[*TestNode]struct{}) error {
	errs := make([]error, len(hosts))
	wg := new(sync.WaitGroup)
	i := 0
	// The following api call is very slow. Using multiple threads speeds that
	// process up a lot.
	for host := range hosts {
		wg.Add(1)
		go func(i int, host *TestNode) {
			errs[i] = host.HostStorageFoldersAddPost(host.Dir, 1048576)
			wg.Done()
		}(i, host)
		i++
	}
	wg.Wait()
	return errors.Compose(errs...)
}

// announceHosts adds storage to each host and announces them to the group
func announceHosts(hosts map[*TestNode]struct{}) error {
	for host := range hosts {
		if err := host.HostAcceptingContractsPost(true); err != nil {
			return errors.AddContext(err, "failed to set host to accepting contracts")
		}
		if err := host.HostAnnouncePost(); err != nil {
			return errors.AddContext(err, "failed to announce host")
		}
	}
	return nil
}

// fullyConnectNodes takes a list of nodes and connects all their gateways
func fullyConnectNodes(nodes []*TestNode) error {
	// Fully connect the nodes
	for i, nodeA := range nodes {
		for _, nodeB := range nodes[i+1:] {
			isPeer, err := nodeA.hasPeer(nodeB)
			if err != nil {
				return build.ExtendErr("couldn't determine if nodeB is a peer of nodeA", err)
			}
			if isPeer {
				continue
			}
			if err := nodeA.GatewayConnectPost(nodeB.GatewayAddress()); err != nil && err != client.ErrPeerExists {
				return errors.AddContext(err, "failed to connect to peer")
			}
		}
	}
	return nil
}

// fundNodes uses the funds of a miner node to fund all the nodes of the group
func fundNodes(miner *TestNode, nodes map[*TestNode]struct{}) error {
	// Get the miner's balance
	wg, err := miner.WalletGet()
	if err != nil {
		return errors.AddContext(err, "failed to get miner's balance")
	}
	// Send txnsPerNode outputs to each node
	txnsPerNode := uint64(25)
	scos := make([]types.SiacoinOutput, 0, uint64(len(nodes))*txnsPerNode)
	funding := wg.ConfirmedSiacoinBalance.Div64(uint64(len(nodes))).Div64(txnsPerNode + 1)
	for node := range nodes {
		wag, err := node.WalletAddressGet()
		if err != nil {
			return errors.AddContext(err, "failed to get wallet address")
		}
		for i := uint64(0); i < txnsPerNode; i++ {
			scos = append(scos, types.SiacoinOutput{
				Value:      funding,
				UnlockHash: wag.Address,
			})
		}
	}
	// Send the transaction
	_, err = miner.WalletSiacoinsMultiPost(scos)
	if err != nil {
		return errors.AddContext(err, "failed to send funding txn")
	}
	// Mine the transactions
	if err := miner.MineBlock(); err != nil {
		return errors.AddContext(err, "failed to mine funding txn")
	}
	// Make sure every node has at least one confirmed transaction
	for node := range nodes {
		err := Retry(100, 100*time.Millisecond, func() error {
			wtg, err := node.WalletTransactionsGet(0, math.MaxInt32)
			if err != nil {
				return err
			}
			if len(wtg.ConfirmedTransactions) == 0 {
				return errors.New("confirmed transactions should be greater than 0")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// hostsInRenterDBCheck makes sure that all the renters see all hosts in their
// database.
func hostsInRenterDBCheck(miner *TestNode, renters map[*TestNode]struct{}, hosts map[*TestNode]struct{}) error {
	for renter := range renters {
		for host := range hosts {
			numRetries := 0
			err := Retry(100, 100*time.Millisecond, func() error {
				numRetries++
				if renter == host {
					// We don't care if the renter is also a host.
					return nil
				}
				// Check if the renter has the host in its db.
				err := errors.AddContext(renter.KnowsHost(host), "renter doesn't know host")
				if err != nil && numRetries%10 == 0 {
					return errors.Compose(err, miner.MineBlock())
				}
				if err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				return build.ExtendErr("not all renters can see all hosts", err)
			}
		}
	}
	return nil
}

// mapToSlice converts a map of TestNodes into a slice
func mapToSlice(m map[*TestNode]struct{}) []*TestNode {
	tns := make([]*TestNode, 0, len(m))
	for tn := range m {
		tns = append(tns, tn)
	}
	return tns
}

// randomDir is a helper functions that returns a random directory path
func randomDir() string {
	dir, err := TestDir(strconv.Itoa(fastrand.Intn(math.MaxInt32)))
	if err != nil {
		panic(errors.AddContext(err, "failed to create testing directory"))
	}
	return dir
}

// setRenterAllowances sets the allowance of each renter
func setRenterAllowances(renters map[*TestNode]struct{}) error {
	for renter := range renters {
		if err := renter.RenterPostAllowance(defaultAllowance); err != nil {
			return err
		}
	}
	return nil
}

// synchronizationCheck makes sure that all the nodes are synced and follow the
func synchronizationCheck(miner *TestNode, nodes map[*TestNode]struct{}) error {
	mcg, err := miner.ConsensusGet()
	if err != nil {
		return err
	}
	// Loop until all the blocks have the same CurrentBlock. If we need to mine
	// a new block in between we need to repeat the check until no block was
	// mined.
	for {
		synced := true
		for node := range nodes {
			err := Retry(1200, 100*time.Millisecond, func() error {
				ncg, err := node.ConsensusGet()
				if err != nil {
					return err
				}
				// If the CurrentBlock's match we are done.
				if mcg.CurrentBlock == ncg.CurrentBlock {
					return nil
				}
				// If the miner's height is greater than the node's we need to
				// wait a bit longer for them to sync.
				if mcg.Height > ncg.Height {
					return errors.New("the node didn't catch up to the miner's height")
				}
				// If the miner's height is smaller to the node's or equal to
				// the node's but still doesn't match, it needs to mine a
				// block.
				synced = false
				return errors.Compose(errors.New("the node's current block does not equal the miner's"),
					miner.MineBlock())
			})
			if err != nil {
				return err
			}
		}
		if synced {
			break
		}
	}
	return nil
}

// waitForContracts waits until the renters have formed contracts with the
// hosts in the group.
func waitForContracts(miner *TestNode, renters map[*TestNode]struct{}, hosts map[*TestNode]struct{}) error {
	expectedContracts := defaultAllowance.Hosts
	if uint64(len(hosts)) < expectedContracts {
		expectedContracts = uint64(len(hosts))
	}
	// Create a map for easier public key lookups.
	hostMap := make(map[string]struct{})
	for host := range hosts {
		pk, err := host.HostPublicKey()
		if err != nil {
			return build.ExtendErr("failed to build hostMap", err)
		}
		hostMap[string(pk.Key)] = struct{}{}
	}
	// each renter is supposed to have at least expectedContracts with hosts
	// from the hosts map.
	for renter := range renters {
		numRetries := 0
		err := Retry(1000, 100, func() error {
			numRetries++
			contracts := uint64(0)
			// Get the renter's contracts.
			rc, err := renter.RenterContractsGet()
			if err != nil {
				return err
			}
			// Count number of contracts
			for _, c := range rc.Contracts {
				if _, exists := hostMap[string(c.HostPublicKey.Key)]; exists {
					contracts++
				}
			}
			// Check if number is sufficient
			if contracts < expectedContracts {
				if numRetries%10 == 0 {
					if err := miner.MineBlock(); err != nil {
						return err
					}
				}
				return errors.New("renter hasn't formed enough contracts")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// AddNodeN adds n nodes of a given template to the group.
func (tg *TestGroup) AddNodeN(np node.NodeParams, n int) error {
	nps := make([]node.NodeParams, n)
	for i := 0; i < n; i++ {
		nps[i] = np
	}
	return tg.AddNodes(nps...)
}

// AddNodes creates a node and adds it to the group.
func (tg *TestGroup) AddNodes(nps ...node.NodeParams) error {
	newNodes := make(map[*TestNode]struct{})
	newHosts := make(map[*TestNode]struct{})
	for _, np := range nps {
		// Create the nodes and add them to the group.
		if np.Dir == "" {
			np.Dir = randomDir()
		}
		node, err := NewCleanNode(np)
		if err != nil {
			return build.ExtendErr("failed to create host", err)
		}
		// Add node to nodes
		tg.nodes[node] = struct{}{}
		// Add node to hosts
		if np.Host != nil || np.CreateHost {
			tg.hosts[node] = struct{}{}
			newHosts[node] = struct{}{}
		}
		// Add node to renters
		if np.Renter != nil || np.CreateRenter {
			tg.renters[node] = struct{}{}
		}
		// Add node to miners
		if np.Miner != nil || np.CreateMiner {
			tg.miners[node] = struct{}{}
		}
		newNodes[node] = struct{}{}
	}

	// Fully connect nodes.
	nodes := mapToSlice(tg.nodes)
	if err := fullyConnectNodes(nodes); err != nil {
		return build.ExtendErr("failed to fully connect nodes", err)
	}
	// Make sure the new nodes are synced.
	miner := mapToSlice(tg.miners)[0]
	if err := synchronizationCheck(miner, tg.nodes); err != nil {
		return build.ExtendErr("synchronization check failed", err)
	}
	// Fund nodes.
	if err := fundNodes(miner, newNodes); err != nil {
		return build.ExtendErr("failed to fund new hosts", err)
	}
	// Add storage to host
	if err := addStorageFolderToHosts(newHosts); err != nil {
		return build.ExtendErr("failed to add storage to hosts", err)
	}
	// Announce host
	if err := announceHosts(newHosts); err != nil {
		return build.ExtendErr("failed to announce hosts", err)
	}
	// Mine a block to get the announcements confirmed
	if err := miner.MineBlock(); err != nil {
		return build.ExtendErr("failed to mine host announcements", err)
	}
	// Block until the hosts show up as active in the renters' hostdbs
	if err := hostsInRenterDBCheck(miner, tg.renters, tg.hosts); err != nil {
		return build.ExtendErr("renter database check failed", err)
	}
	// Wait for all the renters to form contracts if the haven't got enough
	// contracts already.
	if err := waitForContracts(miner, tg.renters, tg.hosts); err != nil {
		return build.ExtendErr("renters failed to form contracts", err)
	}
	// Make sure all nodes are synced
	if err := synchronizationCheck(miner, tg.nodes); err != nil {
		return build.ExtendErr("synchronization check failed", err)
	}
	return nil
}

// Close closes the group and all its nodes. Closing a node is usually a slow
// process, but we can speed it up a lot by closing each node in a separate
// goroutine.
func (tg *TestGroup) Close() error {
	wg := new(sync.WaitGroup)
	errs := make([]error, len(tg.nodes))
	i := 0
	for n := range tg.nodes {
		wg.Add(1)
		go func(i int, n *TestNode) {
			errs[i] = n.Close()
			wg.Done()
		}(i, n)
		i++
	}
	wg.Wait()
	return errors.Compose(errs...)
}

// RemoveNode removes a node from the group and shuts it down.
func (tg *TestGroup) RemoveNode(tn *TestNode) error {
	// Remote node from all data structures.
	delete(tg.nodes, tn)
	delete(tg.hosts, tn)
	delete(tg.renters, tn)
	delete(tg.miners, tn)

	// Close node.
	return tn.Close()
}

// Nodes returns all the nodes of the group
func (tg *TestGroup) Nodes() []*TestNode {
	return mapToSlice(tg.nodes)
}

// Hosts returns all the hosts of the group
func (tg *TestGroup) Hosts() []*TestNode {
	return mapToSlice(tg.hosts)
}

// Renters returns all the renters of the group
func (tg *TestGroup) Renters() []*TestNode {
	return mapToSlice(tg.renters)
}

// Miners returns all the miners of the group
func (tg *TestGroup) Miners() []*TestNode {
	return mapToSlice(tg.miners)
}
