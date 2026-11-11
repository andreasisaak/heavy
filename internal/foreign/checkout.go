package foreign

import "github.com/andreasisaak/heavy/internal/lists"

// MainCheckout is the main checkout of the git checkout that holds directory, the checkout function of the daemon's
// notifier: "" for a directory that is gone or lies in no checkout; git missing or failing to start is an error.
func MainCheckout(directory string) (string, error) {
	checkout, found, err := lists.FindCheckout(directory)
	if err != nil || !found {
		return "", err
	}
	return checkout.Main, nil
}
