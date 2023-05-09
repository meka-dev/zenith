package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peterbourgon/ff/v3"
	"golang.org/x/exp/slices"
)

// This program is meant to be executed on the host running the localnetwork
// instance, and expects to find a "test" keyring at /tmp/smoketest-keyring.
//
// Each network should have a "generate" script in the engine repo that will
// produce a valid keyring to $HOME/.network/keyring-test. For example juno has
// a localjuno/generate.bash which should write $HOME/.junod/keyring-test.
//
// You can run those scripts on the relevant "local" host for the network, and
// then move $HOME/.network/keyring-test to /tmp/smoketest-keyring. But be sure
// to also chmod -R a+r the resulting files, so the smoketest can be run by
// anyone.

type params struct {
	command   string
	chainID   string
	denom     string
	acct1     string
	acct2     string
	blockwait int64 // higher block rate -> longer block wait
}

var osmosisParams = params{
	command:   "osmosisd",
	chainID:   "osmosis-localnet-1",
	denom:     "uosmo",
	acct1:     "osmo10qfrpash5g2vk3hppvu45x0g860czur8ff5yx0",
	acct2:     "osmo1f4tvsdukfwh6s9swrc24gkuz23tp8pd3e9r5fa",
	blockwait: 2,
}

var junoParams = params{
	command:   "junod",
	chainID:   "juno-localnet-1",
	denom:     "ujuno",
	acct1:     "juno1cyyzpxplxdzkeea7kwsydadg87357qnaf5xk87",
	acct2:     "juno18s5lynnmx37hq4wlrw9gdn68sg2uxp5rkl63az",
	blockwait: 2,
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	fs := flag.NewFlagSet("smoketest", flag.ContinueOnError)
	var (
		network   = fs.String("network", "osmosis", "osmosis, juno")
		zenithURL = fs.String("zenith-url", "http://fra-zen-api-dev-0:4411", "URL for zenith-proxy or zenith-<network>")
		blockFile = fs.String("block-file", "", "JSON encoding of block from network mainnet (optional, runs extra test)")
	)
	if err := ff.Parse(fs, os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %v", err)
	}

	var params params
	switch *network {
	case "osmosis":
		params = osmosisParams
	case "juno":
		params = junoParams
	}

	switch {
	case *blockFile != "":
		return runBidBlock(params, *blockFile, *zenithURL)
	default:
		return runBasicFlow(params, *zenithURL)
	}
}

func runBasicFlow(params params, zenithURL string) error {
	log := log.New(log.Writer(), "runBasicFlow: ", log.Flags())

	var tx1Bytes []byte
	{
		tx, err := newBankSendTx(
			params.command,
			params.chainID,
			params.acct1,
			params.acct2,
			"200000"+params.denom,
			"10000"+params.denom,
			"step_1",
		)
		if err != nil {
			return fmt.Errorf("tx 1: %w", err)
		}

		tx1Bytes = tx
	}

	log.Printf("tx1: %s", hashTx(tx1Bytes))

	var tx2Bytes []byte
	{
		tx, err := newBankSendTx(
			params.command,
			params.chainID,
			params.acct2,
			params.acct1,
			"1234"+params.denom,
			"10000"+params.denom,
			"step_2",
		)
		if err != nil {
			return fmt.Errorf("tx 1: %w", err)
		}

		tx2Bytes = tx
	}

	log.Printf("tx2: %s", hashTx(tx2Bytes))

	var latestHeight int64
	var broadcastHeight int64
	var bidHeight int64
	{
		h, err := getHeight(params.command)
		if err != nil {
			return fmt.Errorf("get height: %w", err)
		}

		latestHeight = h
		bidHeight = latestHeight + int64(params.blockwait)
		broadcastHeight = bidHeight - 1
	}

	log.Printf("latest height %d", latestHeight)
	log.Printf("bid at latest height %d + block wait %d = %d", latestHeight, params.blockwait, bidHeight)
	log.Printf("broadcast at bid height %d - 1 = %d", bidHeight, broadcastHeight)

	var bid1Hashes []string
	var bid2Hashes []string
	{
		var (
			url      = zenithURL
			searcher = params.acct1
		)

		h1, err := sendBid(params.command, params.chainID, url, bidHeight, [][]byte{tx2Bytes}, searcher, 1001)
		if err != nil {
			return fmt.Errorf("send bid 1 error: %w", err)
		}
		bid1Hashes = h1

		h2, err := sendBid(params.command, params.chainID, url, bidHeight, [][]byte{tx1Bytes, tx2Bytes}, searcher, 999)
		if err != nil {
			return fmt.Errorf("send bid 2 error: %w", err)
		}
		bid2Hashes = h2
		_ = bid2Hashes // not actually used
	}

	{
		log.Printf("waiting for broadcast height %d", broadcastHeight)
		if _, err := waitForBlock(params.command, params.chainID, broadcastHeight); err != nil {
			return fmt.Errorf("wait for broadcast height: %w", err)
		}
	}

	var tx1Hash string
	var tx1Err error
	var tx2Hash string
	var tx2Err error
	{
		// Broadcast the txs concurrently, because we need to get them in before
		// the next block arrives. For normal txs, order doesn't matter.

		log.Printf("broadcasting txs")
		t0 := time.Now()

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			tx1Hash, tx1Err = broadcastTx(params.command, params.chainID, tx1Bytes, "async")
		}()

		go func() {
			defer wg.Done()
			tx2Hash, tx2Err = broadcastTx(params.command, params.chainID, tx2Bytes, "async")
		}()

		wg.Wait()

		for _, err := range []error{tx1Err, tx2Err} {
			if err != nil {
				return fmt.Errorf("broadcastTx error: %w", err)
			}
		}

		_ = tx2Hash // not actually used directly

		log.Printf("broadcasting done, took %s", time.Since(t0))
	}

	var block *Block
	{
		log.Printf("waiting for block at bid height %d", bidHeight)

		b, err := waitForBlock(params.command, params.chainID, bidHeight)
		if err != nil {
			return fmt.Errorf("wait for block at bid height: %w", err)
		}

		block = b
	}

	var wantHashes []string
	{
		// We expect bid1 to be prioritized and put first, bid2 to be rejected
		// because it has tx2 which is already claimed by bid1, and tx1 to be
		// included at the end of the block via the mempool.

		wantHashes = append(wantHashes, bid1Hashes...) // bid1: tx2, bid1payment1, bid1payment2
		wantHashes = append(wantHashes, tx1Hash)       // mempool: tx1
	}

	var haveHashes []string
	{
		txs := block.Block.Data.Base64Txs
		for i, tx := range txs {
			dectx, err := decodeTx(tx)
			if err != nil {
				return fmt.Errorf("block tx %d/%d: decode error: %w", i+1, len(txs), err)
			}
			haveHashes = append(haveHashes, hashTx(dectx))
		}
	}

	{

		if !slices.Equal(wantHashes, haveHashes) {
			log.Printf("want tx hashes:\n\t%s", strings.Join(wantHashes, "\n\t"))
			log.Printf("block tx hashes:\n\t%s", strings.Join(haveHashes, "\n\t"))
			return fmt.Errorf("block txs didn't match expectation")
		}

		log.Printf("tx hashes matched!")
	}

	return nil
}

func runBidBlock(params params, blockFile string, zenithURL string) error {
	log := log.New(log.Writer(), "runBidBlock: ", log.Flags())

	var txs [][]byte
	{
		log.Printf("get some real txs from %s, presumably from mainnet", blockFile)

		t, err := readBlockFile(blockFile)
		if err != nil {
			return fmt.Errorf("get block txs: %w", err)
		}

		log.Printf("block tx count %d", len(t))

		if len(t) > 10 {
			t = t[:10]
			log.Printf("taking first 10 only")
		}

		if len(t) <= 0 {
			log.Printf("WARNING WARNING WARNING")
			log.Printf("block tx count 0, skipping test")
			log.Printf("WARNING WARNING WARNING")
			return nil
		}

		txs = t
	}

	type bid struct {
		payment  int64
		txhashes []string
	}

	var bidHeight int64
	var bids []bid
	{
		log.Printf("making bids including those txs")

		latestHeight, err := getHeight(params.command)
		if err != nil {
			return fmt.Errorf("get latest height: %w", err)
		}

		bidHeight = latestHeight + params.blockwait

		log.Printf("latest height %d + block wait %d -> bid height %d", latestHeight, params.blockwait, bidHeight)

		bidcount := len(txs) + 1 // see below
		bidc := make(chan bid, bidcount)
		errc := make(chan error, bidcount)

		alreadyPaid := newSyncSet[int64]()
		alreadyPaid.set(0)

		sendbid := func(tx []byte) {
			var (
				url      = zenithURL
				bidTxs   = [][]byte{tx}
				searcher = params.acct1
			)

			var p int64
			for alreadyPaid.get(p) {
				p = int64(1000 + rand.Intn(1000))
			}
			alreadyPaid.set(p)

			hashes, err := sendBid(params.command, params.chainID, url, bidHeight, bidTxs, searcher, p)
			if err == nil {
				bidc <- bid{payment: p, txhashes: hashes}
			} else {
				errc <- err
			}
		}

		sendBidsStart := time.Now()

		for i, tx := range txs {
			log.Printf("sending bid %d/%d", i+1, len(txs))
			go sendbid(tx)

			// Include the last tx in multiple bids, ensure only 1 wins.
			if i == len(txs)-1 {
				log.Printf("sending bid %d/%d (extra)", i+1, len(txs))
				go sendbid(tx)
			}
		}

		for i := 0; i < bidcount; i++ {
			select {
			case bid := <-bidc:
				log.Printf("successful bid %d/%d", i+1, bidcount)
				bids = append(bids, bid)
			case err := <-errc:
				return fmt.Errorf("bid error: %w", err)
			}
		}

		log.Printf("all bids sent in %s", time.Since(sendBidsStart))

	}

	var wantHashes []string
	{
		log.Printf("computing expected block tx order")

		sort.Slice(bids, func(i, j int) bool {
			return bids[i].payment > bids[j].payment
		})

		claimed := map[string]bool{}
		for _, bid := range bids {
			var bidShouldBeRejected bool
			for _, txhash := range bid.txhashes {
				if _, ok := claimed[txhash]; ok {
					bidShouldBeRejected = true
					break
				}
			}
			if bidShouldBeRejected {
				continue
			}
			for _, txhash := range bid.txhashes {
				claimed[txhash] = true
				wantHashes = append(wantHashes, txhash)
			}
		}
	}

	{
		log.Printf("verifying block")

		b, err := waitForBlock(params.command, params.chainID, bidHeight)
		if err != nil {
			return fmt.Errorf("wait for bid block: %w", err)
		}

		var haveHashes []string
		for i, base64tx := range b.Block.Data.Base64Txs {
			tx, err := decodeTx(base64tx)
			if err != nil {
				return fmt.Errorf("block tx %d/%d decode failed: %w", i+1, len(b.Block.Data.Base64Txs), err)
			}
			txhash := fmt.Sprintf("%X", sha256.Sum256(tx))
			haveHashes = append(haveHashes, txhash)
		}

		if !slices.Equal(wantHashes, haveHashes) {
			log.Printf("want tx hashes:\n\t%s", strings.Join(wantHashes, "\n\t"))
			log.Printf("have tx hashes:\n\t%s", strings.Join(haveHashes, "\n\t"))
			return fmt.Errorf("block txs didn't match expectation")
		}

		log.Printf("tx hashes matched!")
	}

	return nil
}

func newBankSendTx(command, chainID, from, to, amount, fees, note string) ([]byte, error) {
	log := log.New(log.Writer(), "newBankSendTx: ", log.Flags())
	defer logDuration(log, time.Now())

	unsigned, err := exec.Command(
		command,
		"--keyring-backend=test",
		"--keyring-dir=/tmp/smoketest-keyring",
		"--chain-id="+chainID,
		"tx", "bank", "send", from, to, amount,
		"--fees="+fees,
		"--note='"+note+"'",
		"--generate-only",
		"--output=json",
		"--yes",
	).CombinedOutput()
	if err != nil {
		log.Print(string(unsigned))
		return nil, fmt.Errorf("generate: %w", err)
	}

	sign := exec.Command(
		command,
		"--keyring-backend=test",
		"--keyring-dir=/tmp/smoketest-keyring",
		"--chain-id="+chainID,
		"tx", "sign", "-",
		"--from="+from,
	)
	sign.Stdin = bytes.NewReader(unsigned)

	signed, err := sign.CombinedOutput()
	if err != nil {
		log.Print(string(signed))
		return nil, fmt.Errorf("sign: %w", err)
	}

	return signed, nil
}

func decodeTx(base64EncodedTx string) ([]byte, error) {
	log := log.New(log.Writer(), "decodeTx: ", log.Flags())
	defer logDuration(log, time.Now())

	// decode := exec.Command(
	// "osmosisd",
	// "--keyring-backend=test",
	// "--keyring-dir=/tmp/smoketest-keyring",
	// "--chain-id="+params.chainID,
	// "tx", "decode", base64EncodedTx,
	// "--output=json",
	// )
	//
	// decode.Stdin = strings.NewReader(base64EncodedTx)
	//
	// out, err := decode.CombinedOutput()
	// if err != nil {
	// log.Printf("osmosisd tx decode: %s", string(out))
	// return nil, fmt.Errorf("decode: %w", err)
	// }

	tx, err := base64.StdEncoding.DecodeString(base64EncodedTx)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	return tx, nil
}

func broadcastTx(command, chainID string, tx []byte, mode string) (string, error) {
	log := log.New(log.Writer(), "broadcastTx: ", log.Flags())
	defer logDuration(log, time.Now())

	broadcast := exec.Command(
		command,
		"--keyring-backend=test",
		"--keyring-dir=/tmp/smoketest-keyring",
		"--chain-id="+chainID,
		"tx", "broadcast", "-",
		"--broadcast-mode="+mode,
		"--output=json",
	)

	broadcast.Stdin = bytes.NewReader(tx)

	out, err := broadcast.CombinedOutput()
	if err != nil {
		log.Printf("tx broadcast: %s", string(out))
		return "", fmt.Errorf("broadcast: %w", err)
	}

	var response struct {
		TxHash string `json:"txhash"`
	}

	json.NewDecoder(bytes.NewReader(out)).Decode(&response)

	return response.TxHash, nil
}

var (
	getHeight = getHeightHTTP
	_         = getHeightSlow
)

func getHeightHTTP(command string) (int64, error) {
	log := log.New(log.Writer(), "getHeight: ", log.Flags())
	// defer logIfSlow(log, time.Now())
	_ = log

	resp, err := http.Get("http://localhost:26657/status")
	if err != nil {
		return 0, fmt.Errorf("get status: %w", err)
	}

	var statusResponse struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statusResponse); err != nil {
		return 0, fmt.Errorf("decode status: %w", err)
	}

	height, _ := strconv.ParseInt(statusResponse.Result.SyncInfo.LatestBlockHeight, 10, 64)
	if height == 0 {
		return 0, fmt.Errorf("height (%s) invalid", statusResponse.Result.SyncInfo.LatestBlockHeight)
	}

	return height, nil
}

func getHeightSlow(command string) (int64, error) {
	log := log.New(log.Writer(), "getHeight: ", log.Flags())
	defer logDuration(log, time.Now())

	status, err := exec.Command(command, "status").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("get status: %w", err)
	}

	var statusResponse struct {
		SyncInfo struct {
			LatestBlockHeight string `json:"latest_block_height"`
		} `json:"SyncInfo"`
	}

	json.NewDecoder(bytes.NewReader(status)).Decode(&statusResponse)

	height, _ := strconv.ParseInt(statusResponse.SyncInfo.LatestBlockHeight, 10, 64)
	if height == 0 {
		return 0, fmt.Errorf("height (%s) invalid", statusResponse.SyncInfo.LatestBlockHeight)
	}

	return height, nil
}

type Bid struct{}

type AuctionResponse struct {
	Payments []struct {
		Address    string  `json:"address"`
		Allocation float64 `json:"allocation"`
		Denom      string  `json:"denom"`
	} `json:"payments"`
}

func sendBid(command, chainID, url string, bidHeight int64, txs [][]byte, from string, totalPayment int64) ([]string, error) {
	log := log.New(log.Writer(), "sendBid: ", log.Flags())
	defer logDuration(log, time.Now())

	// call the auctions endpoint to get payment requirements for height
	// construct a tx containing those payments
	var paymentTxs [][]byte
	{
		body := fmt.Sprintf(` { "chain_id": "`+chainID+`", "height": %d } `, bidHeight)
		req, _ := http.NewRequest("GET", url+"/v0/auction", strings.NewReader(body))
		// req.Header.Add("zenith-chain-id", "localosmosis")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get auction: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("get auction returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var auctionResponse AuctionResponse
		if err := json.NewDecoder(resp.Body).Decode(&auctionResponse); err != nil {
			return nil, fmt.Errorf("decode auction response: %w", err)
		}

		log.Printf("/v0/auction for %d payments: %+v", bidHeight, auctionResponse)

		for i, payment := range auctionResponse.Payments {
			var (
				to     = payment.Address
				share  = int64(float64(totalPayment) * payment.Allocation)
				amount = fmt.Sprintf("%d%s", share, payment.Denom)
				fees   = "10000" + payment.Denom // TODO: probably should be params.denom
				memo   = fmt.Sprintf("payment %d/%d", i+1, len(auctionResponse.Payments))
			)
			tx, err := newBankSendTx(command, chainID, from, to, amount, fees, memo)
			if err != nil {
				return nil, fmt.Errorf("create payment %d/%d: %w", i+1, len(auctionResponse.Payments), err)
			}
			log.Printf("%s (of %d) to %s", amount, totalPayment, to)
			paymentTxs = append(paymentTxs, tx)
		}
	}

	log.Printf("payment tx count %d", len(paymentTxs))

	// submit to the bid endpoint
	var txhashes []string
	{
		bid := struct {
			ChainID string   `json:"chain_id"`
			Height  int64    `json:"height"`
			Kind    string   `json:"kind"`
			Txs     [][]byte `json:"txs"`
		}{
			ChainID: chainID,
			Height:  bidHeight,
			Kind:    "block",
			Txs:     append(txs, paymentTxs...),
		}

		body, err := json.Marshal(bid)
		if err != nil {
			return nil, fmt.Errorf("marshal bid: %w", err)
		}

		req, _ := http.NewRequest("POST", url+"/v0/bid", bytes.NewReader(body))
		// req.Header.Add("zenith-chain-id", "localosmosis")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get post: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ = io.ReadAll(resp.Body)
			return nil, fmt.Errorf("bid returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var bidResp struct {
			TxHashes []string `json:"tx_hashes"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&bidResp); err != nil {
			return nil, fmt.Errorf("decode bid response: %w", err)
		}

		txhashes = bidResp.TxHashes
	}

	return txhashes, nil
}

type Block struct {
	Block struct {
		Header struct {
			ChainID string `json:"chain_id"`
			Height  string `json:"height"`
		} `json:"header"`
		Data struct {
			Base64Txs []string `json:"txs"`
		} `json:"data"`
	} `json:"block"`
}

func readBlockFile(blockFile string) ([][]byte, error) {
	log := log.New(log.Writer(), "readBlockFile: ", log.Flags())
	defer logDuration(log, time.Now())

	body, err := os.ReadFile(blockFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", blockFile, err)
	}

	var b Block
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", blockFile, err)
	}

	log.Printf(
		"%s: %s %s: tx count %d",
		blockFile,
		b.Block.Header.ChainID,
		b.Block.Header.Height,
		len(b.Block.Data.Base64Txs),
	)

	var txs [][]byte
	for i, txstr := range b.Block.Data.Base64Txs {
		tx, err := decodeTx(txstr)
		if err != nil {
			return nil, fmt.Errorf("decode tx %d/%d: %w", i+1, len(b.Block.Data.Base64Txs), err)
		}
		txs = append(txs, tx)
	}

	return txs, nil
}

func waitForBlock(command, chainID string, targetHeight int64) (*Block, error) {
	log := log.New(log.Writer(), "waitForBlock: ", log.Flags())

	var previous int64
	for {
		current, err := getHeight(command)
		if err != nil {
			return nil, fmt.Errorf("get height: %w", err)
		}

		if current >= targetHeight {
			log.Printf("%d = %d", current, targetHeight)
			break
		}

		if current != previous {
			log.Printf("%d < %d", current, targetHeight)
			previous = current
		}

		time.Sleep(25 * time.Millisecond)
	}

	b, err := getBlock(command, chainID, targetHeight)
	if err != nil {
		return nil, fmt.Errorf("get block at height %d: %w", targetHeight, err)
	}

	return b, nil
}

func getBlock(command, chainID string, height int64) (*Block, error) {
	log := log.New(log.Writer(), "getBlock: ", log.Flags())
	defer logDuration(log, time.Now())

	queryblock := exec.Command(
		command,
		"--chain-id="+chainID,
		"query", "block", strconv.FormatInt(height, 10),
	)

	out, err := queryblock.CombinedOutput()
	if err != nil {
		log.Printf("query block: %s", string(out))
		return nil, fmt.Errorf("query block: %w", err)
	}

	var b Block
	if err := json.NewDecoder(bytes.NewReader(out)).Decode(&b); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &b, nil
}

func hashTx(data []byte) string {
	hash := sha256.Sum256(data)
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func logDuration(log *log.Logger, begin time.Time) {
	// log.Printf("took %s", time.Since(begin))
}

type syncSet[K comparable] struct {
	mtx sync.Mutex
	dat map[K]bool
}

func newSyncSet[K comparable]() *syncSet[K] {
	return &syncSet[K]{dat: map[K]bool{}}
}

func (s *syncSet[K]) set(k K) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.dat[k] = true
}

func (s *syncSet[K]) get(k K) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.dat[k]
}
