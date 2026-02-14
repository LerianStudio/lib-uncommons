//go:build unit

package mongo_test

import (
	"fmt"
	"net/url"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/mongo"
)

func ExampleBuildURI() {
	query := url.Values{}
	query.Set("replicaSet", "rs0")

	uri, err := mongo.BuildURI(mongo.URIConfig{
		Scheme:   "mongodb",
		Username: "app",
		Password: "secret",
		Host:     "db.internal",
		Port:     "27017",
		Database: "ledger",
		Query:    query,
	})

	fmt.Println(err == nil)
	fmt.Println(uri)

	// Output:
	// true
	// mongodb://app:secret@db.internal:27017/ledger?replicaSet=rs0
}
