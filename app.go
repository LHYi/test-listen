package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config"
	"github.com/hyperledger/fabric-sdk-go/pkg/gateway"
)

// these address should be changed accordingly when implemented in the hardware
const (
	// the mspID should be identical to the one used when calling cryptogen to generate credential files
	// mspID = "Org1MSP"
	// the path of the certificates
	cryptoPath  = "../fabric-samples-2.3/test-network/organizations/peerOrganizations/org2.example.com"
	certPath    = cryptoPath + "/users/User1@org2.example.com/msp/signcerts/cert.pem"
	keyPath     = cryptoPath + "/users/User1@org2.example.com/msp/keystore/"
	tlsCertPath = cryptoPath + "/peers/peer0.org2.example.com/tls/ca.crt"
	// an IP address to access the peer node, it is a localhost address when the network is running in a single machine
	peerEndpoint = "localhost:9051"
	// name of the peer node
	gatewayPeer = "peer0.org2.example.com"
	// the channel name and the chaincode name should be identical to the ones used in blockchain network implementation, the following are the default values
	// these information have been designed to be entered by the application user
	networkName  = "mychannel"
	contractName = "basic"
	userName     = "appUser"
)

func main() {
	err := os.Setenv("DISCOVERY_AS_LOCALHOST", "true")
	if err != nil {
		log.Fatalf("Error setting DISCOVERY_AS_LOCALHOST environment variable: %v", err)
		os.Exit(1)
	}

	log.Println("============ Creating wallet ============")
	wallet, err := gateway.NewFileSystemWallet("wallet")
	if err != nil {
		log.Fatalf("Failed to create wallet: %v", err)
	}
	log.Println("============ Wallet created ============")

	if !wallet.Exists(userName) {
		err = populateWallet(wallet, userName)
		if err != nil {
			log.Fatalf("->Failed to populate wallet contents: %v", err)
		}
		log.Printf("-> Successfully add user %s to wallet \n", userName)
	} else {
		log.Printf("->  User %s already exists", userName)
	}

	ccpPath := filepath.Join(
		"..",
		"fabric-samples-2.3",
		"test-network",
		"organizations",
		"peerOrganizations",
		"org2.example.com",
		"connection-org2.yaml",
	)

	log.Println("============ connecting to gateway ============")
	gw, err := gateway.Connect(
		gateway.WithConfig(config.FromFile(filepath.Clean(ccpPath))),
		gateway.WithIdentity(wallet, userName),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gateway: %v", err)
	}
	defer gw.Close()
	log.Println("============ Successfully connected to gateway ============")

	network, err := gw.GetNetwork("mychannel")
	if err != nil {
		log.Fatalf("Failed to get network: %v", err)
	}
	log.Println("============ successfully connected to network", networkName, "============")

	contract := network.GetContract(contractName)
	log.Println("============ successfully got contract", contractName, "============")

	eventID := "Org2"
	reg, notifier, err := contract.RegisterEvent(eventID)
	if err != nil {
		fmt.Printf("Failed to register contract event: %s", err)
		return
	}
	defer contract.Unregister(reg)

	var P float64 = 0
	var l1 float64 = 2 * P
	var m1 float64 = 1.5
	var iter int = 0
	var terminate bool = false
iterLoop:
	for {
		select {
		case event := <-notifier:
			fmt.Printf("Received CC event: %s - %s \n", event.EventName, event.Payload)
			iter += 1
			l2 := getLambda(string(event.Payload))
			m2 := getMismatch(string(event.Payload))
			l1, m1, P, terminate = update(l1, l2, m1, m2, P, iter)
			Lambda := fmt.Sprintf("%v", l1)
			Mismatch := fmt.Sprintf("%v", m1)
			_, err := contract.SubmitTransaction("SendUpdate", Lambda, Mismatch)
			if err != nil {
				panic(fmt.Errorf("failed to submit transaction: %w", err))
			}
			if terminate {
				fmt.Printf("Done at iteration %v: P=%v, lambda=%v, mismatch=%v\n", iter, P, l1, m1)
				break iterLoop
			}
		}
	}

	contract.Unregister(reg)

	// funcLoop:
	// 	for {
	// 		fmt.Println("-> Continue?: [y/n] ")
	// 		continueConfirm := catchOneInput()
	// 		if isYes(continueConfirm) {
	// 			invokeFunc(contract)
	// 		} else if isNo(continueConfirm) {
	// 			break funcLoop
	// 		} else {
	// 			fmt.Println("Wrong input")
	// 		}
	// 	}

	// eventReplayLoop:
	// 	for {
	// 		select {
	// 		case event := <-notifier:
	// 			fmt.Printf("Received CC event: %s - %s \n", event.EventName, event.Payload)
	// 			if getLambda(string(event.Payload)) == 1.3456 {
	// 				break eventReplayLoop
	// 			}
	// 			// case <-time.After(1 * time.Second):
	// 			// 	fmt.Printf("No more events\n")
	// 			// 	break eventReplayLoop
	// 		}
	// 	}

	// contract.Unregister(reg)

	fmt.Println("-> Clean up?: [y/n] ")
	cleanUpConfirm := catchOneInput()
	if isYes(cleanUpConfirm) {
		cleanUp()
	}

}

func update(l1 float64, l2 float64, m1 float64, m2 float64, P float64, iter int) (float64, float64, float64, bool) {
	var eta float64 = 1 / float64(iter)
	if eta < 0.05 {
		eta = 0.05
	}
	ltemp := 0.5*l1 + 0.5*l2 + eta*m1
	Ptemp := ltemp / 2
	if Ptemp > 8 {
		Ptemp = 8
	} else if Ptemp < 0 {
		Ptemp = 0
	}
	mtemp := 0.5*m1 + 0.5*m2 + P - Ptemp

	var terminate bool
	if math.Abs(mtemp) < 0.05 && math.Abs(ltemp-l1) < 0.05 {
		terminate = true
	} else {
		terminate = false
	}

	fmt.Printf("Iteration %v: Lambda=%v, Mismatch=%v, P=%v, Terminate=%v\n", iter, ltemp, mtemp, Ptemp, terminate)

	return ltemp, mtemp, Ptemp, terminate
}

func getLambda(s string) float64 {

	pattern := "(?<=Lambda=)[0-9.-]+(?=,)"

	reg, err := regexp2.Compile(pattern, 0)
	if err != nil {
		fmt.Printf("reg: %v, err: %v\n", reg, err)
		return 0
	}

	value, _ := reg.FindStringMatch(s)

	Lambda, errLambda := strconv.ParseFloat(fmt.Sprintf("%v", value), 64)
	if errLambda != nil {
		log.Panic("Error capturing lambda")
	}
	return Lambda
}

func getMismatch(s string) float64 {

	pattern := "(?<=Mismatch=)[0-9.-]+(?=, end)"

	reg, err := regexp2.Compile(pattern, 0)
	if err != nil {
		fmt.Printf("reg: %v, err: %v\n", reg, err)
		return 0
	}

	value, _ := reg.FindStringMatch(s)

	Mismatch, errMismatch := strconv.ParseFloat(fmt.Sprintf("%v", value), 64)
	if errMismatch != nil {
		log.Panic("Error capturing mismatch")
	}

	return Mismatch
}

func populateWallet(wallet *gateway.Wallet, userName string) error {
	credPath := filepath.Join(
		"..",
		"fabric-samples-2.3",
		"test-network",
		"organizations",
		"peerOrganizations",
		"org2.example.com",
		"users",
		"User1@org2.example.com",
		"msp",
	)

	certPath := filepath.Join(credPath, "signcerts", "User1@org2.example.com-cert.pem")
	// read the certificate pem
	cert, err := ioutil.ReadFile(filepath.Clean(certPath))
	if err != nil {
		return err
	}

	keyDir := filepath.Join(credPath, "keystore")
	// there's a single file in this dir containing the private key
	files, err := ioutil.ReadDir(keyDir)
	if err != nil {
		return err
	}
	if len(files) != 1 {
		return fmt.Errorf("keystore folder should have contain one file")
	}
	keyPath := filepath.Join(keyDir, files[0].Name())
	key, err := ioutil.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return err
	}

	identity := gateway.NewX509Identity("Org2MSP", string(cert), string(key))

	return wallet.Put(userName, identity)
}

func cleanUp() {
	log.Println("-> Cleaning up wallet...")
	if _, err := os.Stat("wallet"); err == nil {
		e := os.RemoveAll("wallet")
		if e != nil {
			log.Fatal(e)
		}
	}
	if _, err := os.Stat("keystore"); err == nil {
		e := os.RemoveAll("keystore")
		if e != nil {
			log.Fatal(e)
		}
	}
	log.Println("-> Wallet cleaned up successfully")
}

func invokeFunc(contract *gateway.Contract) {
	var functionName string
	var paraNumber int
	fmt.Println("-> Please enter the name of the smart contract function you want to invoke")
	functionName = catchOneInput()
	fmt.Println("-> Please enter the number of parameters")
	paraNumber, _ = strconv.Atoi(catchOneInput())
	var functionPara []string
	for i := 0; i < paraNumber; i++ {
		fmt.Printf("-> Please enter parameter %v: ", i+1)
		functionPara = append(functionPara, catchOneInput())
	}
	if paraNumber == 0 {
		result, err := contract.SubmitTransaction(functionName)
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}
		fmt.Printf("Result: %s \n", string(result))
	} else {
		result, err := contract.SubmitTransaction(functionName, functionPara...)
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}
		fmt.Printf("Result: %s \n", string(result))
	}
}

func catchOneInput() string {
	// instantiate a new reader
	reader := bufio.NewReader(os.Stdin)
	s, _ := reader.ReadString('\n')
	// get rid of the \n at the end of the string
	s = strings.Replace(s, "\n", "", -1)
	// if the string is exit, exit the application directly
	// this allows the user to exit the application whereever they want and saves the effort of detecting the exit command elsewhere
	if isExit(s) {
		exitApp()
	}
	return s
}

func isYes(s string) bool {
	return strings.Compare(s, "Y") == 0 || strings.Compare(s, "y") == 0 || strings.Compare(s, "Yes") == 0 || strings.Compare(s, "yes") == 0
}

func isNo(s string) bool {
	return strings.Compare(s, "N") == 0 || strings.Compare(s, "n") == 0 || strings.Compare(s, "No") == 0 || strings.Compare(s, "no") == 0
}

func isExit(s string) bool {
	return strings.Compare(s, "Exit") == 0 || strings.Compare(s, "exit") == 0 || strings.Compare(s, "EXIT") == 0
}

func exitApp() {
	log.Println("============ application-golang ends ============")
	// exit code zero indicates that no error occurred
	os.Exit(0)
}

func formatJSON(data []byte) string {
	var result bytes.Buffer
	if err := json.Indent(&result, data, "", "  "); err != nil {
		panic(fmt.Errorf("failed to parse JSON: %w", err))
	}
	return result.String()
}
