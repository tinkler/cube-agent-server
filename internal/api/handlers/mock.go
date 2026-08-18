package handlers

// MockMetaProvider D2 阶段硬编码的元数据
// D3 会被 schema.Registry 提供的真实实现替换
type MockMetaProvider struct{}

// GetMeta 返回 mock 数据,模拟一个 orders cube
func (m *MockMetaProvider) GetMeta() any {
	return map[string]any{
		"cubes": []any{
			map[string]any{
				"name":        "orders",
				"title":       "订单",
				"description": "订单主表(D2 mock,D3 接入真实 schema)",
				"measures": []any{
					map[string]any{
						"name":        "orders.count",
						"title":       "订单数",
						"shortTitle":  "订单数",
						"type":        "count",
						"aggType":     "count",
					},
					map[string]any{
						"name":        "orders.total_amount",
						"title":       "订单总金额",
						"shortTitle":  "总金额",
						"type":        "sum",
						"aggType":     "sum",
						"sql":         "amount",
					},
				},
				"dimensions": []any{
					map[string]any{
						"name":        "orders.id",
						"title":       "订单 ID",
						"type":        "number",
						"primaryKey":  true,
					},
					map[string]any{
						"name":   "orders.status",
						"title":  "订单状态",
						"type":   "string",
					},
					map[string]any{
						"name":   "orders.created_at",
						"title":  "下单时间",
						"type":   "time",
					},
				},
				"segments": []any{},
			},
		},
	}
}
