package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// YOUR EXACT AZURE CLIENT ID
const clientID = "c2025a42-18d5-47d8-a256-be3ef3009623"
const msAuthURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"

var (
	baseDir string
	cliDir  string
	serDir  string
	authF   string
)

type AuthData struct {
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

func initDirs() {
	if runtime.GOARCH != "arm64" {
		fmt.Println("CRITICAL ERROR: ArseniMC is strictly for Apple Silicon ONLY.")
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to locate user home directory: %v\n", err)
		os.Exit(1)
	}
	baseDir = filepath.Join(home, "arsenimc")
	cliDir = filepath.Join(baseDir, "clients")
	serDir = filepath.Join(baseDir, "servers")
	authF = filepath.Join(baseDir, ".auth")

	if err := os.MkdirAll(cliDir, 0755); err != nil {
		fmt.Printf("Failed to create client directory: %v\n", err)
	}
	if err := os.MkdirAll(serDir, 0755); err != nil {
		fmt.Printf("Failed to create server directory: %v\n", err)
	}
}

func main() {
	initDirs()
	if len(os.Args) < 2 {
		fmt.Println("Usage: arsenic [a|r|-r|-d|-s|-m|start]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "a":
		fmt.Println("YOU CAN CHANGE YOUR AVATAR IN THE MINECRAFT WEBSITE OR ANOTHER WEBSITE, NO CLIENT SKIN CHANGING!!!")
		if len(os.Args) == 4 && os.Args[2] == "-k" {
			runFullAuthFlow(os.Args[3])
		} else {
			runFullAuthFlow(doDeviceAuth())
		}
	case "r":
		if err := os.Remove(authF); err == nil || os.IsNotExist(err) {
			fmt.Println("Authentication purged.")
		} else {
			fmt.Printf("Failed to purge auth: %v\n", err)
		}
	case "-r":
		if len(os.Args) > 2 {
			tgt := os.Args[2]
			os.RemoveAll(filepath.Join(cliDir, tgt))
			os.RemoveAll(filepath.Join(serDir, tgt))
			fmt.Printf("Purged installation: %s\n", tgt)
		}
	case "-d":
		chkAuth()
		if len(os.Args) < 5 {
			fmt.Println("Usage: arsenic -d <version> <type> <name>")
			os.Exit(1)
		}
		dlClient(os.Args[2], os.Args[3], os.Args[4])
	case "-s":
		chkAuth()
		if len(os.Args) < 5 {
			fmt.Println("Usage: arsenic -s <version> <type> <name> [port]")
			os.Exit(1)
		}
		prt := "25565"
		if len(os.Args) == 6 {
			prt = os.Args[5]
		}
		dlServer(os.Args[2], os.Args[3], os.Args[4], prt)
	case "-m":
		chkAuth()
		if len(os.Args) < 4 {
			fmt.Println("Usage: arsenic -m <url> <target_name>")
			os.Exit(1)
		}
		dlMod(os.Args[2], os.Args[3])
	case "start":
		chkAuth()
		if len(os.Args) < 4 {
			fmt.Println("Usage: arsenic start [-c|-s] <name>")
			os.Exit(1)
		}
		if os.Args[2] == "-c" {
			startCli(os.Args[3])
		} else if os.Args[2] == "-s" {
			startSer(os.Args[3])
		}
	}
}

func chkAuth() AuthData {
	b, err := os.ReadFile(authF)
	if err != nil {
		fmt.Println("REQUIRES MICROSOFT AUTHENTICATION FIRST!!!!!!!!!!\nRun: arsenic a")
		os.Exit(1)
	}
	var a AuthData
	if err := json.Unmarshal(b, &a); err != nil {
		fmt.Println("Authentication file corrupted. Run: arsenic r\nThen run: arsenic a")
		os.Exit(1)
	}
	return a
}

// ---------------------------------------------------------
// BULLETPROOF AUTHENTICATION ENGINE (OAUTH2)
// ---------------------------------------------------------
func doDeviceAuth() string {
	req, err := http.NewRequest("POST", msAuthURL, strings.NewReader(fmt.Sprintf("client_id=%s&scope=XboxLive.signin offline_access", clientID)))
	if err != nil {
		fmt.Printf("Error building request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("CRITICAL: Network failure reaching Microsoft OAuth.")
		os.Exit(1)
	}
	defer resp.Body.Close()

	var res map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		fmt.Println("CRITICAL: Failed to decode Microsoft OAuth response.")
		os.Exit(1)
	}

	if res["error"] != nil {
		fmt.Printf("\n[MICROSOFT REJECTION] %s\nDetails: %v\n", res["error"], res["error_description"])
		fmt.Println("ACTION REQUIRED: Ensure 'Allow public client flows' is set to YES in Azure Authentication settings.")
		os.Exit(1)
	}

	if res["user_code"] == nil || res["verification_uri"] == nil {
		fmt.Println("CRITICAL: Received malformed payload from Microsoft.")
		os.Exit(1)
	}

	interval := time.Duration(res["interval"].(float64)) * time.Second
	fmt.Printf("\nGo to: %s\nEnter Code: %s\nWaiting...\n", res["verification_uri"], res["user_code"])

	for {
		time.Sleep(interval)
		pr, _ := http.NewRequest("POST", "https://login.microsoftonline.com/consumers/oauth2/v2.0/token", strings.NewReader(fmt.Sprintf("grant_type=urn:ietf:params:oauth:grant-type:device_code&client_id=%s&device_code=%s", clientID, res["device_code"])))
		pr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		pres, err := http.DefaultClient.Do(pr)
		if err != nil {
			continue // Network blip, keep trying
		}

		var t map[string]interface{}
		json.NewDecoder(pres.Body).Decode(&t)
		pres.Body.Close()

		if token, ok := t["access_token"].(string); ok {
			return token
		}

		if t["error"] != nil && t["error"] != "authorization_pending" {
			fmt.Printf("\n[AUTH FAILED] %s\n", t["error"])
			os.Exit(1)
		}
	}
}

func runFullAuthFlow(msToken string) {
	fmt.Println("Executing Xbox Live Handshake...")
	xB, _ := json.Marshal(map[string]interface{}{
		"Properties": map[string]interface{}{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + msToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	})
	req, _ := http.NewRequest("POST", "https://user.auth.xboxlive.com/user/authenticate", bytes.NewBuffer(xB))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("XBL Handshake network error: %v\n", err)
		os.Exit(1)
	}
	defer res.Body.Close()

	var xbl map[string]interface{}
	json.NewDecoder(res.Body).Decode(&xbl)

	if xbl["Token"] == nil {
		fmt.Println("\n[CRITICAL REJECTION] Xbox Live Handshake Failed.")
		fmt.Println("ACTION REQUIRED: Ensure the Microsoft account has a valid Xbox profile (log into xbox.com).")
		os.Exit(1)
	}
	xblToken := xbl["Token"].(string)

	fmt.Println("Exchanging for XSTS Token...")
	sB, _ := json.Marshal(map[string]interface{}{
		"Properties": map[string]interface{}{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	})
	req2, _ := http.NewRequest("POST", "https://xsts.auth.xboxlive.com/xsts/authorize", bytes.NewBuffer(sB))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "application/json")
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		fmt.Printf("XSTS Handshake network error: %v\n", err)
		os.Exit(1)
	}
	defer res2.Body.Close()

	var xsts map[string]interface{}
	json.NewDecoder(res2.Body).Decode(&xsts)

	if xsts["Token"] == nil {
		fmt.Println("\n[CRITICAL REJECTION] XSTS Handshake Failed.")
		if xsts["XErr"] != nil {
			errCode := fmt.Sprintf("%v", xsts["XErr"])
			if errCode == "2148916233" {
				fmt.Println("DIAGNOSIS: No Xbox account found. Sign up at xbox.com.")
			} else if errCode == "2148916238" {
				fmt.Println("DIAGNOSIS: Child account detected. Add to Family and enable multiplayer.")
			}
		}
		os.Exit(1)
	}

	xuiList, ok := xsts["DisplayClaims"].(map[string]interface{})["xui"].([]interface{})
	if !ok || len(xuiList) == 0 {
		fmt.Println("CRITICAL: Invalid XSTS DisplayClaims payload.")
		os.Exit(1)
	}
	uhs := xuiList[0].(map[string]interface{})["uhs"].(string)
	xstsToken := xsts["Token"].(string)

	fmt.Println("Exchanging for Minecraft Token...")
	mB, _ := json.Marshal(map[string]interface{}{"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xstsToken)})
	req3, _ := http.NewRequest("POST", "https://api.minecraftservices.com/authentication/login_with_xbox", bytes.NewBuffer(mB))
	req3.Header.Set("Content-Type", "application/json")
	res3, err := http.DefaultClient.Do(req3)
	if err != nil {
		fmt.Printf("MC Token Handshake network error: %v\n", err)
		os.Exit(1)
	}
	defer res3.Body.Close()

	var mc map[string]interface{}
	json.NewDecoder(res3.Body).Decode(&mc)

	if mc["access_token"] == nil {
		fmt.Println("\n[CRITICAL REJECTION] Minecraft Token Exchange Failed. Ensure you own Minecraft Java.")
		os.Exit(1)
	}
	mcToken := mc["access_token"].(string)

	fmt.Println("Fetching Profile...")
	req4, _ := http.NewRequest("GET", "https://api.minecraftservices.com/minecraft/profile", nil)
	req4.Header.Set("Authorization", "Bearer "+mcToken)
	res4, err := http.DefaultClient.Do(req4)
	if err != nil {
		fmt.Printf("Profile Fetch network error: %v\n", err)
		os.Exit(1)
	}
	defer res4.Body.Close()

	var prof map[string]interface{}
	json.NewDecoder(res4.Body).Decode(&prof)

	if prof["id"] == nil {
		fmt.Println("\n[CRITICAL REJECTION] Profile Fetch Failed. Set a username at minecraft.net.")
		os.Exit(1)
	}

	b, _ := json.Marshal(AuthData{UUID: prof["id"].(string), Name: prof["name"].(string), Token: mcToken})
	if err := os.WriteFile(authF, b, 0644); err != nil {
		fmt.Printf("Failed to save auth token locally: %v\n", err)
	}
	fmt.Printf("\nAuthorization sequence locked. Welcome, %s.\n", prof["name"].(string))
}

// ---------------------------------------------------------
// UNIVERSAL CLIENT DOWNLOADER
// ---------------------------------------------------------
func dlClient(ver, typ, nam string) {
	fmt.Printf("Constructing Client: %s %s -> %s\n", typ, ver, nam)
	mcDir := filepath.Join(cliDir, nam, "minecraft")
	os.MkdirAll(mcDir, 0755)

	resp, err := http.Get("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json")
	if err != nil {
		fmt.Printf("Failed to fetch manifest: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var manifest map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&manifest)

	var vURL string
	versions, ok := manifest["versions"].([]interface{})
	if !ok {
		fmt.Println("Invalid manifest format")
		return
	}
	for _, v := range versions {
		vm := v.(map[string]interface{})
		if vm["id"].(string) == ver {
			vURL = vm["url"].(string)
			break
		}
	}
	if vURL == "" {
		fmt.Printf("Version %s not found in Mojang manifest.\n", ver)
		return
	}

	resp2, err := http.Get(vURL)
	if err != nil {
		fmt.Printf("Failed to fetch version JSON: %v\n", err)
		return
	}
	defer resp2.Body.Close()

	bJSON, _ := io.ReadAll(resp2.Body)
	vDir := filepath.Join(mcDir, "versions", ver)
	os.MkdirAll(vDir, 0755)
	os.WriteFile(filepath.Join(vDir, ver+".json"), bJSON, 0644)

	var vData map[string]interface{}
	json.Unmarshal(bJSON, &vData)

	cURL := vData["downloads"].(map[string]interface{})["client"].(map[string]interface{})["url"].(string)
	if err := dlFile(cURL, filepath.Join(vDir, ver+".jar")); err != nil {
		fmt.Printf("Failed to download client jar: %v\n", err)
		return
	}

	fmt.Println("Downloading Base Assets & ARM64 Natives...")
	libs := vData["libraries"].([]interface{})
	libDir := filepath.Join(mcDir, "libraries")
	natDir := filepath.Join(vDir, "natives")
	os.MkdirAll(libDir, 0755)
	os.MkdirAll(natDir, 0755)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 25)

	for _, l := range libs {
		lib := l.(map[string]interface{})
		dl, ok := lib["downloads"].(map[string]interface{})
		if !ok {
			continue
		}
		if art, ok := dl["artifact"]; ok {
			p := art.(map[string]interface{})["path"].(string)
			u := art.(map[string]interface{})["url"].(string)
			wg.Add(1)
			go func(path, url string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				fullP := filepath.Join(libDir, path)
				os.MkdirAll(filepath.Dir(fullP), 0755)
				if err := dlFile(url, fullP); err == nil {
					if strings.Contains(path, "natives-osx") || strings.Contains(path, "arm64") {
						unzip(fullP, natDir)
					}
				} else {
					fmt.Printf("Failed to download lib %s: %v\n", path, err)
				}
			}(p, u)
		}
	}
	wg.Wait()

	if typ == "fabric" {
		fmt.Println("Injecting Fabric Headless Installer...")
		resp, err := http.Get("https://meta.fabricmc.net/v2/versions/installer")
		if err == nil {
			defer resp.Body.Close()
			var inst []map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&inst)
			if len(inst) > 0 {
				instVer := inst[0]["version"].(string)
				instURL := fmt.Sprintf("https://maven.fabricmc.net/net/fabricmc/fabric-installer/%s/fabric-installer-%s.jar", instVer, instVer)
				instPath := filepath.Join(mcDir, "fabric-installer.jar")
				dlFile(instURL, instPath)
				exec.Command("java", "-jar", instPath, "client", "-mcversion", ver, "-dir", mcDir).Run()
				os.Remove(instPath)
			}
		}
	} else if typ == "forge" {
		fmt.Println("Injecting Forge Headless Installer...")
		forgeURL := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", ver, ver)
		instPath := filepath.Join(mcDir, "forge-installer.jar")
		dlFile(forgeURL, instPath)
		exec.Command("java", "-jar", instPath, "--installClient", mcDir).Run()
		os.Remove(instPath)
	} else if typ == "neoforge" {
		fmt.Println("Injecting NeoForge Headless Installer...")
		neoURL := fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", ver, ver)
		instPath := filepath.Join(mcDir, "neoforge-installer.jar")
		dlFile(neoURL, instPath)
		exec.Command("java", "-jar", instPath, "--installClient", mcDir).Run()
		os.Remove(instPath)
	}
	fmt.Println("Client Architecture Complete.")
}

// ---------------------------------------------------------
// SERVER DOWNLOADER
// ---------------------------------------------------------
func dlServer(ver, typ, nam, prt string) {
	fmt.Printf("Constructing Server %s %s on port %s...\n", typ, ver, prt)
	sDir := filepath.Join(serDir, nam)
	os.MkdirAll(sDir, 0755)
	jarPath := filepath.Join(sDir, "server.jar")

	if typ == "vanilla" {
		resp, err := http.Get("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json")
		if err == nil {
			defer resp.Body.Close()
			var manifest map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&manifest)
			var vURL string
			for _, v := range manifest["versions"].([]interface{}) {
				vm := v.(map[string]interface{})
				if vm["id"].(string) == ver {
					vURL = vm["url"].(string)
					break
				}
			}
			resp2, err2 := http.Get(vURL)
			if err2 == nil {
				defer resp2.Body.Close()
				var vData map[string]interface{}
				json.NewDecoder(resp2.Body).Decode(&vData)
				if serverDl, ok := vData["downloads"].(map[string]interface{})["server"]; ok {
					dlFile(serverDl.(map[string]interface{})["url"].(string), jarPath)
				}
			}
		}
	} else if typ == "paper" {
		resp, err := http.Get(fmt.Sprintf("https://api.papermc.io/v2/projects/paper/versions/%s", ver))
		if err == nil {
			defer resp.Body.Close()
			var pd map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&pd)
			if builds, ok := pd["builds"].([]interface{}); ok && len(builds) > 0 {
				bld := fmt.Sprintf("%.0f", builds[len(builds)-1].(float64))
				dlFile(fmt.Sprintf("https://api.papermc.io/v2/projects/paper/versions/%s/builds/%s/downloads/paper-%s-%s.jar", ver, bld, ver, bld), jarPath)
			}
		}
	} else if typ == "fabric" {
		resp, err := http.Get("https://meta.fabricmc.net/v2/versions/installer")
		if err == nil {
			defer resp.Body.Close()
			var inst []map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&inst)
			if len(inst) > 0 {
				instVer := inst[0]["version"].(string)
				instPath := filepath.Join(sDir, "fabric-installer.jar")
				dlFile(fmt.Sprintf("https://maven.fabricmc.net/net/fabricmc/fabric-installer/%s/fabric-installer-%s.jar", instVer, instVer), instPath)
				exec.Command("java", "-jar", instPath, "server", "-mcversion", ver, "-downloadMinecraft", "-dir", sDir).Run()
				os.Rename(filepath.Join(sDir, "fabric-server-launch.jar"), jarPath)
				os.Remove(instPath)
			}
		}
	}

	os.WriteFile(filepath.Join(sDir, "eula.txt"), []byte("eula=true"), 0644)
	os.WriteFile(filepath.Join(sDir, "server.properties"), []byte(fmt.Sprintf("server-port=%s", prt)), 0644)
	fmt.Println("Server Ignition Ready.")
}

// ---------------------------------------------------------
// LAUNCHERS & MONITORS
// ---------------------------------------------------------
func startCli(nam string) {
	auth := chkAuth()
	mcDir := filepath.Join(cliDir, nam, "minecraft")

	vDir, err := os.ReadDir(filepath.Join(mcDir, "versions"))
	if err != nil {
		fmt.Printf("Failed to read versions directory: %v\n", err)
		return
	}

	var activeVer string
	var mainClass string

	for _, d := range vDir {
		if d.IsDir() {
			if strings.Contains(d.Name(), "fabric") || strings.Contains(d.Name(), "forge") || strings.Contains(d.Name(), "neoforge") {
				activeVer = d.Name()
				break
			}
			activeVer = d.Name()
		}
	}

	bJSON, err := os.ReadFile(filepath.Join(mcDir, "versions", activeVer, activeVer+".json"))
	if err != nil {
		fmt.Printf("Failed to read version JSON: %v\n", err)
		return
	}

	var profileData map[string]interface{}
	json.Unmarshal(bJSON, &profileData)
	if mc, ok := profileData["mainClass"].(string); ok {
		mainClass = mc
	} else {
		fmt.Println("Failed to determine mainClass from version JSON.")
		return
	}

	natDir := filepath.Join(mcDir, "versions", strings.Split(activeVer, "-")[0], "natives")

	var cpStrings []string
	filepath.Walk(filepath.Join(mcDir, "libraries"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".jar") {
			cpStrings = append(cpStrings, p)
		}
		return nil
	})

	baseVer := strings.Split(activeVer, "-")[0]
	cpStrings = append(cpStrings, filepath.Join(mcDir, "versions", baseVer, baseVer+".jar"))
	cp := strings.Join(cpStrings, ":")

	fmt.Println("Igniting Client Instance:", nam)
	fmt.Println("To force quit client, ctrl + c.")

	cmd := exec.Command("java",
		"-XstartOnFirstThread", "-Xmx2G",
		"-Djava.library.path="+natDir,
		"-cp", cp,
		mainClass,
		"--version", activeVer,
		"--gameDir", mcDir,
		"--assetsDir", filepath.Join(mcDir, "assets"),
		"--assetIndex", baseVer,
		"--uuid", auth.UUID,
		"--accessToken", auth.Token,
		"--userType", "msa",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func startSer(nam string) {
	sDir := filepath.Join(serDir, nam)
	sessionName := "arseni_" + nam

	if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err != nil {
		exec.Command("sh", "-c", fmt.Sprintf("tmux new-session -d -s %s -c %s 'java -Xmx2G -jar server.jar nogui'", sessionName, sDir)).Run()
	}

	for {
		if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err != nil {
			fmt.Println("Server terminated.")
			break
		}
		ram, _ := exec.Command("sh", "-c", "top -l 1 | grep PhysMem | awk '{print $2}'").Output()
		cpu, _ := exec.Command("sh", "-c", "top -l 1 | grep 'CPU usage' | awk '{print $3}'").Output()
		fmt.Print("\033[H\033[2J")
		fmt.Printf("=== ArseniMC Server Monitor: %s ===\n", nam)
		fmt.Printf("RAM: %s | CPU: %s\n", strings.TrimSpace(string(ram)), strings.TrimSpace(string(cpu)))
		fmt.Println("Type 'e' + ENTER to input command. Ctrl+C to stop monitor.")

		var in string
		fmt.Scanln(&in)
		if strings.ToLower(in) == "e" {
			fmt.Print("MC Command: ")
			var mc string
			fmt.Scanln(&mc)
			exec.Command("sh", "-c", fmt.Sprintf("tmux send-keys -t %s '%s' C-m", sessionName, mc)).Run()
		}
	}
}

// ---------------------------------------------------------
// UTILS
// ---------------------------------------------------------
func dlMod(url, tgt string) {
	mDir := filepath.Join(cliDir, tgt, "minecraft", "mods")
	if _, err := os.Stat(filepath.Join(serDir, tgt)); err == nil {
		mDir = filepath.Join(serDir, tgt, "mods")
	}
	os.MkdirAll(mDir, 0755)
	if err := dlFile(url, filepath.Join(mDir, filepath.Base(url))); err != nil {
		fmt.Printf("Failed to download mod: %v\n", err)
	} else {
		fmt.Println("Mod downloaded successfully.")
	}
}

func dlFile(url, dest string) error {
	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", r.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, r.Body)
	return err
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	destPath, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	for _, f := range r.File {
		path := filepath.Join(destPath, f.Name)

		// ZIP SLIP VULNERABILITY FIX: Ensure extraction stays inside the intended target directory
		if !strings.HasPrefix(filepath.Clean(path)+string(os.PathSeparator), filepath.Clean(destPath)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path inside archive: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
