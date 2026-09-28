package cmdshared

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

func PromptYesNo(prompt string) (bool, error) {
	fmt.Print(prompt)
	if viper.GetBool("non-interactive") {
		fmt.Println("Y (non-interactive mode)")
		return true, nil
	}
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to prompt user: %w", err)
	}

	ansNormal := strings.ToLower(strings.TrimSpace(answer))
	if len(ansNormal) > 0 && ansNormal[0] == 'n' {
		return false, nil
	}
	return true, nil
}
