package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/renproject/mercury/sdk/client/ethclient"
	"github.com/renproject/mercury/types/ethtypes"
	"github.com/republicprotocol/darknode-cli/darknode"
	"github.com/republicprotocol/darknode-cli/darknode/bindings"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// destroyNode tears down the deployed darknode by its name.
func destroyNode(ctx *cli.Context) error {
	force := ctx.Bool("force")
	name := ctx.Args().First()
	if name == "" {
		cli.ShowCommandHelp(ctx, "down")
		return ErrEmptyNodeName
	}

	// Parse the node config
	nodePath := nodePath(name)
	config, err := darknode.NewConfigFromJSONFile(filepath.Join(nodePath, "config.json"))
	if err != nil {
		return err
	}

	// Connect to Ethereum
	client, err := connect(config.Network)
	if err != nil {
		return err
	}
	dnr, err := bindings.NewDarknodeRegistry(config.DNRAddress, client.EthClient())
	if err != nil {
		return err
	}
	ethAddr := crypto.PubkeyToAddress(config.Keystore.Ecdsa.PublicKey)

	// Check if the node is registered
	if err := checkRegistered(dnr, ethAddr); err != nil {
		return err
	}
	// Check if the node is in pending registration/deregistration stage
	if err := checkPendingStage(dnr, ethAddr); err != nil {
		return err
	}
	// Check if the darknode has been refunded
	context, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	refunded, err := dnr.IsRefunded(&bind.CallOpts{Context: context}, ethAddr)
	if err != nil {
		return err
	}
	if !refunded {
		fmt.Println("You haven't refund your darknode. Please refund your darknode from the command center")
		return nil
	}

	// Check if user want to process without extra confirmation
	if !force {
		fmt.Println("Do you really want to destroy your darknode? (Yes/No)")

		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		input := strings.ToLower(strings.TrimSpace(text))
		if input != "yes" && input != "y" {
			return nil
		}
	}
	fmt.Printf("%sDestroying your darknode ...%s\n", RESET, RESET)

	destroy := fmt.Sprintf("cd %v && terraform destroy --force && find . -type f -not -name 'config.json' -delete", nodePath)
	return run("bash", "-c", destroy)
}

// Withdraw ETH and REN in the darknode address to the provided receiver address
func withdraw(ctx *cli.Context) error {
	name := ctx.Args().First()
	withdrawAddress := ctx.String("address")

	// Validate the name and received ethereum address
	nodePath, err := validateDarknodeName(name)
	if err != nil {
		return err
	}
	receiverAddr, err := stringToEthereumAddress(withdrawAddress)
	if err != nil {
		return err
	}

	// Parse the node config
	config, err := darknode.NewConfigFromJSONFile(filepath.Join(nodePath, "config.json"))
	if err != nil {
		return err
	}

	// Connect to Ethereum
	client, err := connect(config.Network)
	if err != nil {
		return err
	}

	// Create a transactor for ethereum tx
	ethAddr := crypto.PubkeyToAddress(config.Keystore.Ecdsa.PublicKey)
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	auth := bind.NewKeyedTransactor(config.Keystore.Ecdsa.PrivateKey)
	auth.GasPrice = big.NewInt(5000000000) // Set GasPrise to 5 Gwei
	auth.Context = c

	// Check REN balance first
	renAddress := renAddress(config.Network)
	tokenContract, err := bindings.NewERC20(common.HexToAddress(renAddress), client.EthClient())
	if err != nil {
		return err
	}
	renBalance, err := tokenContract.BalanceOf(&bind.CallOpts{}, ethAddr)
	if err != nil {
		return err
	}

	// Withdraw REN if the darknode has more than 1 REN.
	fmt.Println("Checking REN balance...")
	oneREN := big.NewInt(1e18)
	if renBalance.Cmp(oneREN) > 0 {
		tx, err := tokenContract.Transfer(auth, receiverAddr, renBalance)
		if err != nil {
			return err
		}
		receipt, err := bind.WaitMined(c, client.EthClient(), tx)
		if err != nil {
			return err
		}
		renBalanceNoDecimals := big.NewInt(0).Div(renBalance, oneREN)
		fmt.Printf("%s%v REN has been withdrawn from your darknode to [%v]. TxHash: %v.%s\n", GREEN, renBalanceNoDecimals.Int64(), receiverAddr.Hex(), receipt.TxHash.Hex(), RESET)
	} else {
		fmt.Printf("%sYour account doesn't have REN token.%s\n", GREEN, RESET)
	}

	// Check the ETH balance
	fmt.Println("Checking ETH balance...")
	balance, err := client.Balance(c, ethtypes.Address(ethAddr))
	if err != nil {
		return err
	}
	gas := ethtypes.Gwei(5 * 21000)
	zero := ethtypes.Wei(0)
	if balance.Gt(zero) {
		if balance.Gt(gas) {
			tx, err := transfer(auth, receiverAddr, balance.Sub(gas), client)
			if err != nil {
				return err
			}
			fmt.Printf("%sYour ETH has been withdrawn from your darknode to [%v]. TxHash: %v.%s\n", GREEN, receiverAddr.Hex(), tx.Hash().Hex(), RESET)
		} else {
			return fmt.Errorf("%sYour account has %v wei which is not enough to cover the transaction fee %v on ethereum.%s\n", RED, balance, gas, RESET)
		}
	} else {
		fmt.Printf("%sYour don't have any ETH left in your account.%s\n", GREEN, RESET)
	}
	return nil
}

// transfer ETH to
func transfer(transactor *bind.TransactOpts, receiver common.Address, amount ethtypes.Amount, client ethclient.Client) (*types.Transaction, error) {
	bound := bind.NewBoundContract(receiver, abi.ABI{}, nil, client.EthClient(), nil)
	transactor.Value = amount.ToBig()
	transactor.GasLimit = 21000
	return bound.Transfer(transactor)
}

// renAddress on different network
func renAddress(network darknode.Network) string {
	switch network {
	case darknode.Mainnet, darknode.Chaosnet:
		return "0x408e41876cCCDC0F92210600ef50372656052a38"
	case darknode.Testnet, darknode.Devnet:
		return "0x2CD647668494c1B15743AB283A0f980d90a87394"
	default:
		panic("unknown network")
	}
}

func connect(network darknode.Network) (ethclient.Client, error) {
	logger := logrus.New()
	switch network {
	case darknode.Mainnet, darknode.Chaosnet:
		return ethclient.New(logger, ethtypes.Mainnet)
	case darknode.Testnet, darknode.Devnet:
		return ethclient.New(logger, ethtypes.Kovan)
	default:
		return nil, errors.New("unknown network")
	}
}

func checkRegistered(dnr *bindings.DarknodeRegistry, addr common.Address) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	registered, err := dnr.IsRegistered(&bind.CallOpts{Context: ctx}, addr)
	if err != nil {
		return err
	}
	if registered {
		fmt.Printf("%sYour node hasn't been deregistered%s\n", RED, RESET)
		fmt.Printf("%sPlease go to darknode command center to deregister your darknode.%s\n", RED, RESET)
		return fmt.Errorf("%s\nPlease try again after you fully deregister your node%s\n", RED, RESET)
	}
	return nil
}

func checkPendingStage(dnr *bindings.DarknodeRegistry, addr common.Address) error {
	reCtx, reCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer reCancel()
	pendingRegistration, err := dnr.IsPendingRegistration(&bind.CallOpts{Context: reCtx}, addr)
	if err != nil {
		return err
	}
	if pendingRegistration {
		return fmt.Errorf("%sYour node is currently in pending registration stage, please deregister your node after next epoch shuffle%s\n", RED, RESET)
	}

	deCtx, deCancel := context.WithTimeout(context.Background(), time.Minute)
	defer deCancel()
	pendingDeregistration, err := dnr.IsPendingDeregistration(&bind.CallOpts{Context: deCtx}, addr)
	if err != nil {
		return err
	}
	if pendingDeregistration {
		return fmt.Errorf("%sYour node is currently in pending deregistration stage, please wait for next epoch shuffle and try again%s\n", RED, RESET)
	}

	return nil
}
