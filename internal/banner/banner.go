package banner

import "fmt"

// Logo is the ASCII art logo for Pilot
const Logo = `
   ██████╗ ██╗██╗      ██████╗ ████████╗
   ██╔══██╗██║██║     ██╔═══██╗╚══██╔══╝
   ██████╔╝██║██║     ██║   ██║   ██║
   ██╔═══╝ ██║██║     ██║   ██║   ██║
   ██║     ██║███████╗╚██████╔╝   ██║
   ╚═╝     ╚═╝╚══════╝ ╚═════╝    ╚═╝
`

// Tagline is the project tagline
const Tagline = "AI That Ships Your Tickets"

// Print prints the banner with tagline
func Print() {
	fmt.Println(Logo)
	fmt.Printf("   %s\n\n", Tagline)
}

// PrintWithVersion prints the banner with version info
func PrintWithVersion(version string) {
	fmt.Println(Logo)
	fmt.Printf("   %s\n", Tagline)
	fmt.Printf("   v%s\n\n", version)
}

// PrintCompact prints a compact single-line banner
func PrintCompact() {
	fmt.Println("🚀 Pilot - AI That Ships Your Tickets")
}

// StartupBanner prints the full startup banner
func StartupBanner(version, gateway string) {
	fmt.Println(Logo)
	fmt.Printf("   %s\n", Tagline)
	fmt.Println()
	fmt.Printf("   Version:  v%s\n", version)
	fmt.Printf("   Gateway:  %s\n", gateway)
	fmt.Println()
}
