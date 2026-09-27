package main

import (
	// Modules of packwiz
	"github.com/andre-carbajal/packwiz/cmd"
	_ "github.com/andre-carbajal/packwiz/curseforge"
	_ "github.com/andre-carbajal/packwiz/github"
	_ "github.com/andre-carbajal/packwiz/migrate"
	_ "github.com/andre-carbajal/packwiz/modrinth"
	_ "github.com/andre-carbajal/packwiz/settings"
	_ "github.com/andre-carbajal/packwiz/url"
	_ "github.com/andre-carbajal/packwiz/utils"
)

func main() {
	cmd.Execute()
}
