package api
import "testing"
func TestNaira(t *testing.T) {
	for _, c := range []struct{ k int64; want string }{
		{0, "₦0.00"}, {100, "₦1.00"}, {4750000, "₦47,500.00"},
		{102500000, "₦1,025,000.00"}, {950, "₦9.50"}, {1, "₦0.01"},
		{-500000, "-₦5,000.00"},
	} {
		if got := naira(c.k); got != c.want { t.Errorf("naira(%d) = %s, want %s", c.k, got, c.want) }
	}
	if got := prettyMethod("ACCOUNT_TRANSFER"); got != "Account transfer" { t.Errorf("got %q", got) }
}
