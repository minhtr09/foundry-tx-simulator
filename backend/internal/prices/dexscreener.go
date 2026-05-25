package prices

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"foundry-tx-simulator/backend/internal/fundflow"
)

const dexScreenerBaseURL = "https://api.dexscreener.com/tokens/v1/"

type DexScreenerProvider struct {
	Client  *http.Client
	BaseURL string
}

func (p DexScreenerProvider) Fetch(ctx context.Context, chain string, tokens []string) (map[string]fundflow.TokenPrice, error) {
	chainID := dexScreenerChain(chain)
	if chainID == "" {
		return nil, nil
	}
	tokens = normalizeTokens(tokens)
	if len(tokens) == 0 {
		return nil, nil
	}

	wanted := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		wanted[token] = struct{}{}
	}

	endpoint := trimBaseURL(p.BaseURL, dexScreenerBaseURL) + "/" + url.PathEscape(chainID) + "/" + strings.Join(tokens, ",")
	var pairs []struct {
		BaseToken struct {
			Address string `json:"address"`
			Symbol  string `json:"symbol"`
		} `json:"baseToken"`
		Liquidity *struct {
			USD float64 `json:"usd"`
		} `json:"liquidity"`
		Info *struct {
			ImageURL string `json:"imageUrl"`
		} `json:"info"`
	}
	if err := fetchJSON(ctx, p.Client, endpoint, "dexscreener price", &pairs, nil); err != nil {
		return nil, err
	}

	best := make(map[string]dexScreenerCandidate)
	for _, pair := range pairs {
		liquidityUSD := 0.0
		if pair.Liquidity != nil {
			liquidityUSD = pair.Liquidity.USD
		}
		baseCandidate := dexScreenerCandidate{
			liquidity: liquidityUSD,
			symbol:    strings.TrimSpace(pair.BaseToken.Symbol),
		}
		if pair.Info != nil {
			baseCandidate.logoURL = strings.TrimSpace(pair.Info.ImageURL)
		}
		addDexScreenerCandidate(best, wanted, normalizeAddress(pair.BaseToken.Address), baseCandidate)
	}

	out := make(map[string]fundflow.TokenPrice)
	for token, candidate := range best {
		out[token] = fundflow.TokenPrice{
			Symbol:  candidate.symbol,
			LogoURL: candidate.logoURL,
		}
	}
	return out, nil
}

func addDexScreenerCandidate(best map[string]dexScreenerCandidate, wanted map[string]struct{}, token string, candidate dexScreenerCandidate) {
	if token == "" {
		return
	}
	if _, ok := wanted[token]; !ok {
		return
	}
	if current, ok := best[token]; !ok || candidate.liquidity > current.liquidity {
		best[token] = candidate
	}
}

type dexScreenerCandidate struct {
	liquidity float64
	symbol    string
	logoURL   string
}

func dexScreenerChain(chain string) string {
	switch normalizeChain(chain) {
	case "mainnet", "eth", "ethereum":
		return "ethereum"
	case "arbitrum-one", "arbitrum_one":
		return "arbitrum"
	case "optimism", "optimistic-ethereum", "op":
		return "optimism"
	case "polygon-pos", "matic":
		return "polygon"
	case "binance-smart-chain", "bnb":
		return "bsc"
	case "avalanche", "avax":
		return "avalanche"
	default:
		return normalizeChain(chain)
	}
}
