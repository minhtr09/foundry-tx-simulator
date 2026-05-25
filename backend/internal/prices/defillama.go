package prices

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"foundry-tx-simulator/backend/internal/fundflow"

	"golang.org/x/crypto/sha3"
)

const defillamaBaseURL = "https://coins.llama.fi/prices/current/"

type DefiLlamaProvider struct {
	Client  *http.Client
	BaseURL string
}

func (p DefiLlamaProvider) Fetch(ctx context.Context, chain string, tokens []string) (map[string]fundflow.TokenPrice, error) {
	coins := defillamaCoinIDs(chain, tokens)
	if len(coins) == 0 {
		return nil, nil
	}

	var payload struct {
		Coins map[string]struct {
			Price    float64 `json:"price"`
			Decimals int     `json:"decimals"`
			Symbol   string  `json:"symbol"`
		} `json:"coins"`
	}
	endpoint := trimBaseURL(p.BaseURL, defillamaBaseURL) + "/" + strings.Join(coins, ",")
	if err := fetchJSON(ctx, p.Client, endpoint, "defillama price", &payload, nil); err != nil {
		return nil, err
	}

	out := make(map[string]fundflow.TokenPrice)
	for coinID, price := range payload.Coins {
		if price.Price <= 0 {
			slog.Warn("defillama price missing usd", "chain", chain, "coin_id", coinID, "price_usd", price.Price, "symbol", strings.TrimSpace(price.Symbol))
			continue
		}
		_, token, ok := strings.Cut(coinID, ":")
		if !ok {
			continue
		}
		normalizedToken := strings.ToLower(token)
		out[normalizedToken] = fundflow.TokenPrice{
			PriceUSD:    price.Price,
			Decimals:    price.Decimals,
			HasDecimals: true,
			Symbol:      strings.TrimSpace(price.Symbol),
			LogoURL:     trustWalletLogoURL(chain, token),
		}
		slog.Info("defillama price fetched", "chain", chain, "token", normalizedToken, "price_usd", price.Price, "decimals", price.Decimals, "symbol", strings.TrimSpace(price.Symbol))
	}
	for _, coinID := range coins {
		_, token, ok := strings.Cut(coinID, ":")
		if !ok {
			continue
		}
		normalizedToken := strings.ToLower(token)
		if _, ok := out[normalizedToken]; !ok {
			slog.Warn("defillama price missing token", "chain", chain, "token", normalizedToken, "coin_id", coinID)
		}
	}
	return out, nil
}

func defillamaCoinIDs(chain string, tokens []string) []string {
	llamaChain := defillamaChain(chain)
	seen := make(map[string]struct{}, len(tokens))
	coins := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		coin := llamaChain + ":" + token
		if _, ok := seen[coin]; ok {
			continue
		}
		seen[coin] = struct{}{}
		coins = append(coins, coin)
	}
	sort.Strings(coins)
	return coins
}

func defillamaChain(chain string) string {
	switch normalizeChain(chain) {
	case "mainnet", "eth", "ethereum":
		return "ethereum"
	case "arbitrum", "arbitrum-one", "arbitrum_one":
		return "arbitrum"
	default:
		return normalizeChain(chain)
	}
}

func trustWalletLogoURL(chain string, token string) string {
	checksum := checksumAddress(token)
	if checksum == "" {
		return ""
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/trustwallet/assets/master/blockchains/%s/assets/%s/logo.png", defillamaChain(chain), checksum)
}

func checksumAddress(value string) string {
	address := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(address) != 40 || !isLowerHex(address) {
		return ""
	}

	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte(address))
	hash := hasher.Sum(nil)

	var out strings.Builder
	out.Grow(42)
	out.WriteString("0x")
	for i := 0; i < len(address); i++ {
		char := address[i]
		if char >= '0' && char <= '9' {
			out.WriteByte(char)
			continue
		}

		nibble := hash[i/2]
		if i%2 == 0 {
			nibble >>= 4
		} else {
			nibble &= 0x0f
		}
		if nibble >= 8 {
			out.WriteByte(char - 32)
		} else {
			out.WriteByte(char)
		}
	}
	return out.String()
}

func isLowerHex(value string) bool {
	for i := 0; i < len(value); i++ {
		char := value[i]
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}
