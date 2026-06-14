package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/ricardoedgarsilva/terraform-provider-linuxhost/internal/provider"
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(), providerserver.ServeOpts{
		Address: "registry.terraform.io/ricardoedgarsilva/linuxhost",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
