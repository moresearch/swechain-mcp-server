package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	commandTimeout = 30 * time.Second
	pageLimit      = 50
	maxPages       = 10 // Reduced to prevent runaway queries
	requestDelay   = 500 * time.Millisecond
	maxRetries     = 3
)

var swechaindCmd string

// Core data structures
type Key struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type Auction struct {
	ID          int    `json:"id,string"`
	Issue       string `json:"issue"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Winner      string `json:"winner"`
	Creator     string `json:"creator"`
}

type Bid struct {
	AuctionID   int    `json:"auctionId,string"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
	Creator     string `json:"creator"`
	Bidder      string `json:"bidder"`
}

type DenomOwner struct {
	Address string `json:"address"`
	Balance struct {
		Amount string `json:"amount"`
		Denom  string `json:"denom"`
	} `json:"balance"`
}

type Balance struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

// Enhanced response structures
type AuctionSummaryResponse struct {
	Summary string `json:"summary"`
	Details struct {
		Auctions     []AuctionDetail     `json:"auctions"`
		Participants []ParticipantDetail `json:"participants"`
	} `json:"details"`
}

type AuctionDetail struct {
	AuctionID        int         `json:"auctionId"`
	Issue            string      `json:"issue"`
	Creator          string      `json:"creator"`
	Description      string      `json:"description"`
	Status           string      `json:"status"`
	Winner           string      `json:"winner"`
	CurrentBidAmount string      `json:"currentBidAmount"`
	Bids             []BidDetail `json:"bids"`
}

type BidDetail struct {
	Bidder      string `json:"bidder"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
}

type ParticipantDetail struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Balance string `json:"balance"`
}

type KeySummaryResponse struct {
	Summary string `json:"summary"`
	Details struct {
		Keys []Key `json:"keys"`
	} `json:"details"`
}

type BlockchainStatusResponse struct {
	Summary string `json:"summary"`
	Details struct {
		TotalAuctions int `json:"totalAuctions"`
		OpenAuctions  int `json:"openAuctions"`
		TotalBids     int `json:"totalBids"`
		TotalKeys     int `json:"totalKeys"`
		TokenHolders  int `json:"tokenHolders"`
	} `json:"details"`
}

type BalanceResponse struct {
	Summary string `json:"summary"`
	Details struct {
		Address  string    `json:"address"`
		Balances []Balance `json:"balances"`
	} `json:"details"`
}

// Parameter structures - all with required fields for proper schema generation
type GetAddressForKeyParams struct {
	KeyName string `json:"keyName"`
}

type GetBalanceParams struct {
	Address string `json:"address"`
}

type QueryOpenAuctionsParams struct {
	Operation string `json:"operation"`
}

type QueryAllAuctionsParams struct {
	Operation string `json:"operation"`
}

type QueryBidsForAuctionParams struct {
	AuctionId string `json:"auctionId"`
}

type GetBlockchainStatusParams struct {
	Operation string `json:"operation"`
}

type GetKeysParams struct {
	Operation string `json:"operation"`
}

type OpenAuctionParams struct {
	Issue       string `json:"issue"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
	Winner      string `json:"winner,omitempty"`
	From        string `json:"from"`
}

type CreateBidParams struct {
	AuctionId   string `json:"auctionId"`
	Bidder      string `json:"bidder"`
	Amount      string `json:"amount,omitempty"`
	Description string `json:"description,omitempty"`
	From        string `json:"from"`
}

type PayParams struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Amount string `json:"amount"`
}

type CloseAuctionParams struct {
	AuctionId   string `json:"auctionId"`
	Status      string `json:"status"`
	Issue       string `json:"issue"`
	Description string `json:"description"`
	Winner      string `json:"winner"`
	From        string `json:"from"`
}

type CreateAndFundAddressParams struct {
	KeyName       string `json:"keyName"`
	FunderAddress string `json:"funderAddress"`
	Amount        string `json:"amount,omitempty"`
}

func init() {
	log.SetOutput(os.Stdout)

	cmd, err := exec.LookPath("swechaind")
	if err != nil {
		log.Fatal("swechaind not found in PATH")
	}
	swechaindCmd = cmd
	log.Printf("Found swechaind at: %s", swechaindCmd)
}

// Enhanced command execution with retry logic
func runCommand(name string, arg ...string) (string, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)

		cmd := exec.CommandContext(ctx, name, arg...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if i == 0 {
			log.Printf("Executing command: %s %v", name, arg)
		} else {
			log.Printf("Retry %d: Executing command: %s %v", i+1, name, arg)
		}

		err := cmd.Run()
		cancel()

		if err == nil {
			result := strings.TrimSpace(stdout.String())
			log.Printf("Command succeeded: %s", result)
			return result, nil
		}

		lastErr = fmt.Errorf("command failed: %v\nSTDOUT: %s\nSTDERR: %s",
			err, stdout.String(), stderr.String())

		if i < maxRetries-1 {
			time.Sleep(time.Duration(i+1) * time.Second) // Exponential backoff
		}
	}

	log.Printf("Command failed after %d retries: %v", maxRetries, lastErr)
	return "", lastErr
}

// Enhanced address validation
func isValidCosmosAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	return strings.HasPrefix(addr, "cosmos1") && len(addr) >= 39 && len(addr) <= 45
}

func main() {
	flag.Parse()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "swechain-mcp-server",
		Version: "1.0.0",
	}, nil)

	// Register enhanced tools with consistent descriptions
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get-address-for-key",
		Description: "Get the cosmos address for a specific key name. Required parameter: keyName (string).",
	}, getAddressForKeyHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get-balance",
		Description: "Get token balance for a specific cosmos address. Required parameter: address (string).",
	}, getBalanceHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query-open-auctions",
		Description: "Get all open auctions with detailed bid information and participants. Required parameter: operation (use 'list').",
	}, queryOpenAuctionsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query-all-auctions",
		Description: "Get all auctions (open and closed) with detailed information. Required parameter: operation (use 'list').",
	}, queryAllAuctionsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "query-bids-for-auction",
		Description: "Get bids for a specific auction or all bids. Required parameter: auctionId (string - use specific ID or 'all').",
	}, queryBidsForAuctionHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get-blockchain-status",
		Description: "Get overall blockchain statistics. Required parameter: operation (use 'status').",
	}, getBlockchainStatusHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get-keys",
		Description: "Get all keys in the keyring with addresses. Required parameter: operation (use 'list').",
	}, getKeysHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "open-auction",
		Description: "Create a new auction. Required: issue, description, from. Optional: status, winner.",
	}, openAuctionHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create-bid",
		Description: "Place a bid on an auction. Required: auctionId, bidder, from. Optional: amount, description.",
	}, createBidHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "pay",
		Description: "Send tokens between addresses. Required: from, to, amount (all must be valid).",
	}, payHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "close-auction",
		Description: "Close/update an auction. Required: auctionId, status, issue, description, winner, from.",
	}, closeAuctionHandler)

	/*
		mcp.AddTool(server, &mcp.Tool{
			Name:        "create-and-fund-address",
			Description: "Use only for new users. Create new key and fund it. Required: keyName, funderAddress. Optional: amount.",
		}, createAndFundAddressHandler)
	*/

	log.Println("MCP server starting on stdio")
	if err := server.Run(context.Background(), mcp.NewStdioTransport()); err != nil {
		log.Printf("Server error: %v", err)
	}
	log.Println("MCP server stopped")
}

// Enhanced handlers with better error handling and validation

func getAddressForKeyHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[GetAddressForKeyParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Getting address for key: %s", params.Arguments.KeyName)

	keyName := strings.TrimSpace(params.Arguments.KeyName)
	if keyName == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: keyName parameter is required and cannot be empty."}},
		}, nil
	}

	address, err := getAddressForKey(keyName)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error getting address for key %s: %v", keyName, err)}},
		}, nil
	}

	response := map[string]interface{}{
		"summary": fmt.Sprintf("Address for key '%s': %s", keyName, address),
		"details": map[string]interface{}{
			"keyName": keyName,
			"address": address,
		},
	}

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func getBalanceHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[GetBalanceParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Getting balance for address: %s", params.Arguments.Address)

	address := strings.TrimSpace(params.Arguments.Address)
	if address == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: address parameter is required and cannot be empty."}},
		}, nil
	}

	if !isValidCosmosAddress(address) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: address must be a valid cosmos address (cosmos1...)."}},
		}, nil
	}

	balances, err := getBalanceForAddress(address)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error getting balance for address %s: %v", address, err)}},
		}, nil
	}

	// Calculate total balance summary
	var totalBalance string = "0"
	var denom string = "token"

	if len(balances) > 0 {
		totalBalance = balances[0].Amount
		denom = balances[0].Denom
	}

	response := BalanceResponse{
		Summary: fmt.Sprintf("Address %s has %s %s", address, totalBalance, denom),
		Details: struct {
			Address  string    `json:"address"`
			Balances []Balance `json:"balances"`
		}{
			Address:  address,
			Balances: balances,
		},
	}

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func queryOpenAuctionsHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[QueryOpenAuctionsParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Querying open auctions")

	// Get data with error handling
	rawAuctions := fetchPaginatedData("issuemarket", "list-auction", "Auction")
	rawBids := fetchPaginatedData("issuemarket", "list-bid", "Bid")
	owners := fetchDenomOwners()

	auctions := parseAuctions(rawAuctions)
	bids := parseBids(rawBids)

	// Filter open auctions
	var openAuctions []Auction
	for _, auction := range auctions {
		if strings.ToLower(strings.TrimSpace(auction.Status)) == "open" {
			openAuctions = append(openAuctions, auction)
		}
	}

	// Build enhanced response
	response := buildAuctionSummaryResponse(openAuctions, bids, owners, "open")

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func queryAllAuctionsHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[QueryAllAuctionsParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Querying all auctions")

	rawAuctions := fetchPaginatedData("issuemarket", "list-auction", "Auction")
	rawBids := fetchPaginatedData("issuemarket", "list-bid", "Bid")
	owners := fetchDenomOwners()

	auctions := parseAuctions(rawAuctions)
	bids := parseBids(rawBids)

	response := buildAuctionSummaryResponse(auctions, bids, owners, "all")

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func queryBidsForAuctionHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[QueryBidsForAuctionParams]) (*mcp.CallToolResultFor[any], error) {
	auctionId := strings.TrimSpace(params.Arguments.AuctionId)
	log.Printf("INFO: Querying bids for auction: %s", auctionId)

	if auctionId == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: auctionId parameter is required. Use specific auction ID or 'all' for all bids."}},
		}, nil
	}

	rawBids := fetchPaginatedData("issuemarket", "list-bid", "Bid")
	bids := parseBids(rawBids)

	// Filter by auction if not 'all'
	if strings.ToLower(auctionId) != "all" {
		auctionIdInt, err := strconv.Atoi(auctionId)
		if err != nil {
			return &mcp.CallToolResultFor[any]{
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Error: invalid auctionId '%s'. Must be a number or 'all'.", auctionId)}},
			}, nil
		}

		var filteredBids []Bid
		for _, bid := range bids {
			if bid.AuctionID == auctionIdInt {
				filteredBids = append(filteredBids, bid)
			}
		}
		bids = filteredBids
	}

	var summary string
	if strings.ToLower(auctionId) == "all" {
		summary = fmt.Sprintf("Found %d total bids across all auctions", len(bids))
	} else {
		summary = fmt.Sprintf("Found %d bids for auction %s", len(bids), auctionId)
	}

	response := map[string]interface{}{
		"summary": summary,
		"details": map[string]interface{}{
			"bids": bids,
		},
	}

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func getBlockchainStatusHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[GetBlockchainStatusParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Getting blockchain status")

	rawAuctions := fetchPaginatedData("issuemarket", "list-auction", "Auction")
	rawBids := fetchPaginatedData("issuemarket", "list-bid", "Bid")
	owners := fetchDenomOwners()
	keys := getKeys()

	auctions := parseAuctions(rawAuctions)
	bids := parseBids(rawBids)

	// Count open auctions
	openCount := 0
	for _, auction := range auctions {
		if strings.ToLower(strings.TrimSpace(auction.Status)) == "open" {
			openCount++
		}
	}

	response := BlockchainStatusResponse{
		Summary: fmt.Sprintf("Blockchain has %d total auctions (%d open), %d bids, %d keys, and %d token holders",
			len(auctions), openCount, len(bids), len(keys), len(owners)),
		Details: struct {
			TotalAuctions int `json:"totalAuctions"`
			OpenAuctions  int `json:"openAuctions"`
			TotalBids     int `json:"totalBids"`
			TotalKeys     int `json:"totalKeys"`
			TokenHolders  int `json:"tokenHolders"`
		}{
			TotalAuctions: len(auctions),
			OpenAuctions:  openCount,
			TotalBids:     len(bids),
			TotalKeys:     len(keys),
			TokenHolders:  len(owners),
		},
	}

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func getKeysHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[GetKeysParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Getting all keys")

	keys := getKeys()

	response := KeySummaryResponse{
		Summary: fmt.Sprintf("Found %d keys in the keyring", len(keys)),
		Details: struct {
			Keys []Key `json:"keys"`
		}{
			Keys: keys,
		},
	}

	result, _ := json.MarshalIndent(response, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(result)}},
	}, nil
}

func openAuctionHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[OpenAuctionParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Handling 'open-auction' tool request. Params: %+v", params.Arguments)

	// Validate required parameters
	issue := strings.TrimSpace(params.Arguments.Issue)
	description := strings.TrimSpace(params.Arguments.Description)
	from := strings.TrimSpace(params.Arguments.From)

	if issue == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'issue' parameter is required and cannot be empty."}},
		}, nil
	}
	if description == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'description' parameter is required and cannot be empty."}},
		}, nil
	}
	if from == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from' parameter is required and cannot be empty."}},
		}, nil
	}

	if !isValidCosmosAddress(from) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from' must be a valid cosmos address (cosmos1...)."}},
		}, nil
	}

	// Set defaults for optional parameters
	status := strings.TrimSpace(params.Arguments.Status)
	if status == "" {
		status = "open"
	}

	winner := strings.TrimSpace(params.Arguments.Winner)

	args := []string{
		"tx", "issuemarket", "create-auction",
		issue,
		description,
		status,
		winner,
		"--from", from,
		"--keyring-backend", "test",
		"--chain-id", "swechain",
		"--fees", "200token",
		"--yes",
		"--output", "json",
	}

	output, err := runCommand(swechaindCmd, args...)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to create auction: %v\nOutput: %s", err, output)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: output}},
	}, nil
}

func createBidHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[CreateBidParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Handling 'create-bid' tool request. Params: %+v", params.Arguments)

	// Validate required parameters
	auctionId := strings.TrimSpace(params.Arguments.AuctionId)
	bidder := strings.TrimSpace(params.Arguments.Bidder)
	from := strings.TrimSpace(params.Arguments.From)

	if auctionId == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'auctionId' parameter is required."}},
		}, nil
	}
	if bidder == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'bidder' parameter is required."}},
		}, nil
	}
	if from == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from' parameter is required."}},
		}, nil
	}

	// Validate addresses
	if !isValidCosmosAddress(bidder) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'bidder' must be a valid cosmos address (cosmos1...)."}},
		}, nil
	}
	if !isValidCosmosAddress(from) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from' must be a valid cosmos address (cosmos1...)."}},
		}, nil
	}

	// Validate auction ID is numeric
	if _, err := strconv.Atoi(auctionId); err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'auctionId' must be a valid number."}},
		}, nil
	}

	// Set defaults
	amount := strings.TrimSpace(params.Arguments.Amount)
	if amount == "" {
		amount = "100token"
	}

	description := strings.TrimSpace(params.Arguments.Description)
	if description == "" {
		description = fmt.Sprintf("Bid for auction %s", auctionId)
	}

	args := []string{
		"tx", "issuemarket", "create-bid",
		auctionId,
		bidder,
		amount,
		description,
		"--from", from,
		"--keyring-backend", "test",
		"--chain-id", "swechain",
		"--fees", "200token",
		"--yes",
		"--output", "json",
	}

	output, err := runCommand(swechaindCmd, args...)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to create bid: %v\nOutput: %s", err, output)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: output}},
	}, nil
}

func payHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[PayParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Handling 'pay' tool request")

	from := strings.TrimSpace(params.Arguments.From)
	to := strings.TrimSpace(params.Arguments.To)
	amount := strings.TrimSpace(params.Arguments.Amount)

	if from == "" || to == "" || amount == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from', 'to', and 'amount' parameters are all required."}},
		}, nil
	}

	if !isValidCosmosAddress(from) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from' must be a valid cosmos address."}},
		}, nil
	}
	if !isValidCosmosAddress(to) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'to' must be a valid cosmos address."}},
		}, nil
	}

	args := []string{
		"tx", "bank", "send",
		from, to, amount,
		"--from", from,
		"--keyring-backend", "test",
		"--chain-id", "swechain",
		"--fees", "200token",
		"--yes",
		"--output", "json",
	}

	output, err := runCommand(swechaindCmd, args...)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Payment failed: %v\nOutput: %s", err, output)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: output}},
	}, nil
}

func closeAuctionHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[CloseAuctionParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Handling 'close-auction' tool request")

	auctionId := strings.TrimSpace(params.Arguments.AuctionId)
	status := strings.TrimSpace(params.Arguments.Status)
	from := strings.TrimSpace(params.Arguments.From)

	// Validate required parameters
	if auctionId == "" || status == "" || from == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'auctionId', 'status', and 'from' parameters are required."}},
		}, nil
	}

	if !isValidCosmosAddress(from) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'from' must be a valid cosmos address."}},
		}, nil
	}

	if _, err := strconv.Atoi(auctionId); err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'auctionId' must be a valid number."}},
		}, nil
	}

	args := []string{
		"tx", "issuemarket", "update-auction",
		auctionId,
		strings.TrimSpace(params.Arguments.Issue),
		strings.TrimSpace(params.Arguments.Description),
		status,
		strings.TrimSpace(params.Arguments.Winner),
		"--from", from,
		"--keyring-backend", "test",
		"--chain-id", "swechain",
		"--fees", "200token",
		"--yes",
		"--output", "json",
	}

	output, err := runCommand(swechaindCmd, args...)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to close auction: %v\nOutput: %s", err, output)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: output}},
	}, nil
}

/*
func createAndFundAddressHandler(ctx context.Context, sess *mcp.ServerSession, params *mcp.CallToolParamsFor[CreateAndFundAddressParams]) (*mcp.CallToolResultFor[any], error) {
	log.Printf("INFO: Handling 'create-and-fund-address' tool request")

	keyName := strings.TrimSpace(params.Arguments.KeyName)
	funderAddress := strings.TrimSpace(params.Arguments.FunderAddress)

	if keyName == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'keyName' parameter is required."}},
		}, nil
	}
	if funderAddress == "" {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'funderAddress' parameter is required."}},
		}, nil
	}

	if !isValidCosmosAddress(funderAddress) {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Error: 'funderAddress' must be a valid cosmos address."}},
		}, nil
	}

	amount := strings.TrimSpace(params.Arguments.Amount)
	if amount == "" {
		amount = "1000token"
	}

	// Create the key
	createArgs := []string{
		"keys", "add", keyName,
		"--keyring-backend", "test",
		"--output", "json",
	}

	output, err := runCommand(swechaindCmd, createArgs...)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to create key: %v\nOutput: %s", err, output)}},
		}, nil
	}

	var keyData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &keyData); err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to parse key creation output: %v", err)}},
		}, nil
	}

	newAddress, ok := keyData["address"].(string)
	if !ok {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: "Failed to extract address from key creation output"}},
		}, nil
	}

	// Fund the new address
	fundArgs := []string{
		"tx", "bank", "send",
		funderAddress, newAddress, amount,
		"--from", funderAddress,
		"--keyring-backend", "test",
		"--chain-id", "swechain",
		"--fees", "200token",
		"--yes",
		"--output", "json",
	}

	fundOutput, err := runCommand(swechaindCmd, fundArgs...)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Key created but funding failed: %v\nKey: %s\nAddress: %s", err, keyName, newAddress)}},
		}, nil
	}

	result := fmt.Sprintf("Successfully created and funded address:\nKey: %s\nAddress: %s\nAmount: %s\nFunding result: %s",
		keyName, newAddress, amount, fundOutput)

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}
*/
// Helper functions with enhanced error handling

func getAddressForKey(keyName string) (string, error) {
	output, err := runCommand(swechaindCmd, "keys", "show", keyName, "--keyring-backend", "test", "--output", "json")
	if err != nil {
		return "", fmt.Errorf("failed to get key info: %w", err)
	}

	var keyData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &keyData); err != nil {
		return "", fmt.Errorf("failed to parse key data: %w", err)
	}

	address, ok := keyData["address"].(string)
	if !ok {
		return "", fmt.Errorf("address not found in key data")
	}

	if !isValidCosmosAddress(address) {
		return "", fmt.Errorf("invalid cosmos address format: %s", address)
	}

	return address, nil
}

func getBalanceForAddress(address string) ([]Balance, error) {
	output, err := runCommand(swechaindCmd, "query", "bank", "balances", address, "--keyring-backend", "test", "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to query balance: %w", err)
	}

	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &responseData); err != nil {
		return nil, fmt.Errorf("failed to parse balance data: %w", err)
	}

	rawBalances, ok := responseData["balances"].([]interface{})
	if !ok {
		return []Balance{}, nil // No balances found
	}

	var balances []Balance
	for _, raw := range rawBalances {
		rawMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		amount := fmt.Sprintf("%v", rawMap["amount"])
		denom := fmt.Sprintf("%v", rawMap["denom"])

		balances = append(balances, Balance{
			Amount: amount,
			Denom:  denom,
		})
	}

	return balances, nil
}

func getKeys() []Key {
	output, err := runCommand(swechaindCmd, "keys", "list", "--keyring-backend", "test", "--output", "json")
	if err != nil {
		log.Printf("Error fetching keys: %v", err)
		return []Key{}
	}

	var keys []Key
	if err := json.Unmarshal([]byte(output), &keys); err != nil {
		log.Printf("Error parsing keys: %v", err)
		return []Key{}
	}

	return keys
}

func buildAuctionSummaryResponse(auctions []Auction, bids []Bid, owners []DenomOwner, auctionType string) AuctionSummaryResponse {
	var auctionDetails []AuctionDetail

	for _, auction := range auctions {
		var auctionBids []BidDetail
		var currentBidAmount string = "0token"

		for _, bid := range bids {
			if bid.AuctionID == auction.ID {
				auctionBids = append(auctionBids, BidDetail{
					Bidder:      bid.Bidder,
					Amount:      bid.Amount,
					Description: bid.Description,
				})
				currentBidAmount = bid.Amount
			}
		}

		auctionDetails = append(auctionDetails, AuctionDetail{
			AuctionID:        auction.ID,
			Issue:            auction.Issue,
			Creator:          auction.Creator,
			Description:      auction.Description,
			Status:           auction.Status,
			Winner:           auction.Winner,
			CurrentBidAmount: currentBidAmount,
			Bids:             auctionBids,
		})
	}

	var participants []ParticipantDetail
	for _, owner := range owners {
		participants = append(participants, ParticipantDetail{
			Name:    extractNameFromAddress(owner.Address),
			Address: owner.Address,
			Balance: fmt.Sprintf("%s %s", owner.Balance.Amount, owner.Balance.Denom),
		})
	}

	var summary string
	if auctionType == "open" {
		bidCount := 0
		for _, detail := range auctionDetails {
			if len(detail.Bids) > 0 {
				bidCount++
			}
		}

		if len(auctions) == 0 {
			summary = "No open auctions found."
		} else {
			summary = fmt.Sprintf("There are %d open auctions (%d with bids, %d without bids).",
				len(auctions), bidCount, len(auctions)-bidCount)
		}
	} else {
		summary = fmt.Sprintf("There are %d total auctions with %d total bids.", len(auctions), len(bids))
	}

	return AuctionSummaryResponse{
		Summary: summary,
		Details: struct {
			Auctions     []AuctionDetail     `json:"auctions"`
			Participants []ParticipantDetail `json:"participants"`
		}{
			Auctions:     auctionDetails,
			Participants: participants,
		},
	}
}

func extractNameFromAddress(address string) string {
	if len(address) >= 12 {
		return address[7:12]
	}
	return address
}

func fetchPaginatedData(module, query, dataKey string) []map[string]interface{} {
	var allResults []map[string]interface{}
	offset := 0

	for offset/pageLimit < maxPages {
		args := []string{
			"query", module, query,
			"--keyring-backend", "test",
			"--output", "json",
			"--page-offset", strconv.Itoa(offset),
			"--page-limit", strconv.Itoa(pageLimit),
		}

		output, err := runCommand(swechaindCmd, args...)
		if err != nil {
			log.Printf("Error fetching %s/%s offset %d: %v", module, query, offset, err)
			break
		}

		var responseData map[string]interface{}
		if err := json.Unmarshal([]byte(output), &responseData); err != nil {
			log.Printf("Error parsing %s/%s offset %d: %v", module, query, offset, err)
			break
		}

		results, ok := responseData[dataKey].([]interface{})
		if !ok || len(results) == 0 {
			break
		}

		for _, result := range results {
			if resultMap, ok := result.(map[string]interface{}); ok {
				allResults = append(allResults, resultMap)
			}
		}

		offset += pageLimit
		time.Sleep(requestDelay)
	}

	return allResults
}

func fetchDenomOwners() []DenomOwner {
	output, err := runCommand(swechaindCmd, "query", "bank", "denom-owners", "token", "--keyring-backend", "test", "--output", "json")
	if err != nil {
		log.Printf("Error fetching denom owners: %v", err)
		return []DenomOwner{}
	}

	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(output), &responseData); err != nil {
		log.Printf("Error parsing denom owners: %v", err)
		return []DenomOwner{}
	}

	rawDenomOwners, ok := responseData["denom_owners"].([]interface{})
	if !ok {
		log.Printf("Unexpected format for denom owners")
		return []DenomOwner{}
	}

	var denomOwners []DenomOwner
	for _, raw := range rawDenomOwners {
		rawMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		address := fmt.Sprintf("%v", rawMap["address"])
		balanceRaw, ok := rawMap["balance"].(map[string]interface{})
		if !ok {
			continue
		}

		amount := fmt.Sprintf("%v", balanceRaw["amount"])
		denom := fmt.Sprintf("%v", balanceRaw["denom"])

		denomOwners = append(denomOwners, DenomOwner{
			Address: address,
			Balance: struct {
				Amount string `json:"amount"`
				Denom  string `json:"denom"`
			}{
				Amount: amount,
				Denom:  denom,
			},
		})
	}

	return denomOwners
}

func parseAuctions(rawData []map[string]interface{}) []Auction {
	var auctions []Auction
	for _, raw := range rawData {
		idStr := fmt.Sprintf("%v", raw["id"])
		id, _ := strconv.Atoi(idStr)

		auction := Auction{
			ID:          id,
			Issue:       fmt.Sprintf("%v", raw["issue"]),
			Description: fmt.Sprintf("%v", raw["description"]),
			Status:      fmt.Sprintf("%v", raw["status"]),
			Winner:      fmt.Sprintf("%v", raw["winner"]),
			Creator:     fmt.Sprintf("%v", raw["creator"]),
		}
		auctions = append(auctions, auction)
	}
	return auctions
}

func parseBids(rawData []map[string]interface{}) []Bid {
	var bids []Bid
	for _, raw := range rawData {
		auctionIDStr := fmt.Sprintf("%v", raw["auctionId"])
		auctionID, _ := strconv.Atoi(auctionIDStr)

		bid := Bid{
			AuctionID:   auctionID,
			Amount:      fmt.Sprintf("%v", raw["amount"]),
			Description: fmt.Sprintf("%v", raw["description"]),
			Creator:     fmt.Sprintf("%v", raw["creator"]),
			Bidder:      fmt.Sprintf("%v", raw["bidder"]),
		}
		bids = append(bids, bid)
	}
	return bids
}
