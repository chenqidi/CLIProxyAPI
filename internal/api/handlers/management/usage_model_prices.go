package management

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// usage-model-prices: map[string]config.UsageModelPrice
func (h *Handler) GetUsageModelPrices(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusOK, gin.H{"usage-model-prices": gin.H{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usage-model-prices": config.NormalizeUsageModelPrices(h.cfg.UsageModelPrices)})
}

func (h *Handler) PutUsageModelPrices(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	var entries map[string]config.UsageModelPrice
	if err = json.Unmarshal(data, &entries); err != nil {
		var wrapper struct {
			Items  map[string]config.UsageModelPrice `json:"items"`
			Prices map[string]config.UsageModelPrice `json:"usage-model-prices"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		switch {
		case len(wrapper.Prices) > 0:
			entries = wrapper.Prices
		default:
			entries = wrapper.Items
		}
	}

	h.cfg.UsageModelPrices = config.NormalizeUsageModelPrices(entries)
	h.persist(c)
}

// usage-price-selected-model: string
func (h *Handler) GetUsagePriceSelectedModel(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusOK, gin.H{
			"usage-price-selected-model": config.DefaultUsagePriceModel,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"usage-price-selected-model": config.NormalizeUsagePriceSelectedModel(h.cfg.UsagePriceSelectedModel),
	})
}

func (h *Handler) PutUsagePriceSelectedModel(c *gin.Context) {
	h.updateStringField(c, func(v string) {
		h.cfg.UsagePriceSelectedModel = config.NormalizeUsagePriceSelectedModel(v)
	})
}
