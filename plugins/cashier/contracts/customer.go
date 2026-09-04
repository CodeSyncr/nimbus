package contracts

import "context"

// CustomerParams creates or updates a gateway customer. A stable customer is
// what lets a subscription and a saved card outlive a single checkout.
type CustomerParams struct {
	Subject  string // your user id
	Email    string
	Name     string
	Phone    string
	Metadata map[string]string
}

// Customer is a gateway customer reduced to what Nimbus mirrors.
type Customer struct {
	Gateway string
	ID      string
	Email   string
	Subject string
	Raw     map[string]any
}

// CustomerGateway is implemented by gateways with a persistent customer object.
type CustomerGateway interface {
	CreateCustomer(ctx context.Context, p CustomerParams) (*Customer, error)
	GetCustomer(ctx context.Context, id string) (*Customer, error)
}
