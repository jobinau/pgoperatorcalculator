package main

import (
	"fmt"

	PG "github.com/jobinau/pgoperatorcalculator/src/pgoperatorcalculator"
)

func main() {
	var conf PG.Configuration
	conf.Init()

	request := PG.ConfigurationRequest{
		Output:          PG.ResultOutputFormatJson,
		PGVersion:       PG.Version{Major: 17, Minor: 6, Patch: 0},
		DBType:          PG.DbTypeOLTP,
		TotalMemory:     32,
		TotalMemoryUnit: PG.MemoryUnitGB,
		CPUNum:          8,
		Connections:     500,
		HDType:          PG.HDTypeSSD,
		DBSize:          PG.DBSizeMidRAM,
		Backup:          PG.BackupConfiguration{Enabled: true},
	}

	var calculator PG.PGOperatorCalculator
	calculator.Init(request, conf)
	err, message, families := calculator.GetCalculate()
	if err != nil {
		fmt.Printf("calculation failed: %v\n", err)
		return
	}
	output, err := calculator.GetJSONOutput(message, calculator.IncomingRequest, families)
	if err != nil {
		fmt.Printf("output failed: %v\n", err)
		return
	}
	fmt.Println(output.String())
}
