package runtime

import "github.com/LerianStudio/lib-uncommons/uncommons/log"

func init() {
	log.SetProductionModeResolver(IsProductionMode)
}
