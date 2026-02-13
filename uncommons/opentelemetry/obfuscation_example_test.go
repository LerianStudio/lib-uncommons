package opentelemetry_test

import (
	"encoding/json"
	"fmt"

	"github.com/LerianStudio/lib-uncommons/uncommons/opentelemetry"
)

func ExampleObfuscateStruct_customRules() {
	redactor, err := opentelemetry.NewRedactor([]opentelemetry.RedactionRule{
		{FieldPattern: `(?i)^password$`, Action: opentelemetry.RedactionMask},
		{FieldPattern: `(?i)^email$`, Action: opentelemetry.RedactionHash},
	}, "***")
	if err != nil {
		fmt.Println("invalid rules")
		return
	}

	masked, err := opentelemetry.ObfuscateStruct(map[string]any{
		"name":     "alice",
		"email":    "a@b.com",
		"password": "secret",
	}, redactor)
	if err != nil {
		fmt.Println("obfuscation failed")
		return
	}

	b, _ := json.Marshal(masked)
	fmt.Println(string(b))

	// Output:
	// {"email":"sha256:fb98d44ad7501a959f3f4f4a3f004fe2d9e581ea6207e218c4b02c08a4d75adf","name":"alice","password":"***"}
}
