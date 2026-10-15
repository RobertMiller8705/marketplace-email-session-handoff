package marketplace

import "testing"

func TestBuyerUpdateControlsHandoff(t *testing.T) {
	tests := []struct {
		name, buyer, update, wantState string
		wantErr                        bool
	}{
		{"accepted buyer receives asset", "buyer-7", "accepted", "handed_off", false},
		{"pending update keeps custody", "buyer-7", "pending", "", true},
		{"different buyer cannot accept", "buyer-8", "accepted", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStore()
			store.PutOrder(Order{ID: "order-42", SellerID: "seller-3", BuyerID: "buyer-7", Asset: "dataset.tar", State: "asset_ready"})
			got, err := store.ApplyBuyerUpdate("order-42", tt.buyer, tt.update)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.State != tt.wantState {
				t.Fatalf("state = %q, want %q", got.State, tt.wantState)
			}
		})
	}
}
