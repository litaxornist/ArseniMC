package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

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
	home, _ := os.UserHomeDir()
	baseDir = filepath.Join(home, "arsenimc")
	cliDir = filepath.Join(baseDir, "clients")
	serDir = filepath.Join(baseDir, "servers")
	authF = filepath.Join(baseDir, ".auth")

	os.MkdirAll(cliDir, 0755)
	os.MkdirAll(serDir, 0755)
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
		os.Remove(authF)
		fmt.Println("Authentication purged.")
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
			fmt.Println("name:MANDATORY")
			os.Exit(1)
		}
		dlClient(os.Args[2], os.Args[3], os.Args[4])
	case "-s":
		chkAuth()
		if len(os.Args) < 5 {
			fmt.Println("name:MANDATORY")
			os.Exit(1)
		}
		prt := "25565"
		if len(os.Args) == 6 {
			prt = os.Args[5]
		}
		dlServer(os.Args[2], os.Args[3], os.Args[4], prt)
	case "-m":
		chkAuth()
		dlMod(os.Args[2], os.Args[3])
	case "start":
		chkAuth()
		if os.Args[2] == "-c" {
			startCli(os.Args[3])
		} else if os.Args[2] == "-s" {
			startSer(os.Args[3])
		}
	}
}

func chkAuth() AuthData {
	b, err := ioutil.ReadFile(authF)
	if err != nil {
		fmt.Println("REQUIRES MICROSOFT AUTHENTICATION FIRST!!!!!!!!!!\nRun: arsenic a")
		os.Exit(1)
	}
	var a AuthData
	json.Unmarshal(b, &a)
	return a
}

// ---------------------------------------------------------
// UNIVERSAL CLIENT DOWNLOADER (VANILLA, FABRIC, FORGE, NEOFORGE)
// ---------------------------------------------------------
func dlClient(ver, typ, nam string) {
	fmt.Printf("Constructing Client: %s %s -> %s\n", typ, ver, nam)
	mcDir := filepath.Join(cliDir, nam, "minecraft")
	os.MkdirAll(mcDir, 0755)

	// 1. Always fetch Vanilla Base First (Required for all modloaders)
	resp, _ := http.Get("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json")
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

	resp2, _ := http.Get(vURL)
	bJSON, _ := ioutil.ReadAll(resp2.Body)
	vDir := filepath.Join(mcDir, "versions", ver)
	os.MkdirAll(vDir, 0755)
	ioutil.WriteFile(filepath.Join(vDir, ver+".json"), bJSON, 0644)

	var vData map[string]interface{}
	json.Unmarshal(bJSON, &vData)

	cURL := vData["downloads"].(map[string]interface{})["client"].(map[string]interface{})["url"].(string)
	dlFile(cURL, filepath.Join(vDir, ver+".jar"))

	// Concurrently fetch Base Libraries and Assets
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
		dl := lib["downloads"].(map[string]interface{})
		if art, ok := dl["artifact"]; ok {
			p := art.(map[string]interface{})["path"].(string)
			u := art.(map[string]interface{})["url"].(string)
			wg.Add(1)
			go func(path, url string) {
				defer wg.Done()
				sem <- struct{}{}
				fullP := filepath.Join(libDir, path)
				os.MkdirAll(filepath.Dir(fullP), 0755)
				dlFile(url, fullP)
				if strings.Contains(path, "natives-osx") || strings.Contains(path, "arm64") {
					unzip(fullP, natDir)
				}
				<-sem
			}(p, u)
		}
	}
	wg.Wait()

	// 2. Modloader Injectors
	if typ == "fabric" {
		fmt.Println("Injecting Fabric Headless Installer...")
		resp, _ := http.Get("https://meta.fabricmc.net/v2/versions/installer")
		var inst []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&inst)
		instVer := inst[0]["version"].(string)

		instURL := fmt.Sprintf("https://maven.fabricmc.net/net/fabricmc/fabric-installer/%s/fabric-installer-%s.jar", instVer, instVer)
		instPath := filepath.Join(mcDir, "fabric-installer.jar")
		dlFile(instURL, instPath)

		exec.Command("java", "-jar", instPath, "client", "-mcversion", ver, "-dir", mcDir).Run()
		os.Remove(instPath)

	} else if typ == "forge" {
		fmt.Println("Injecting Forge Headless Installer...")
		// Forge doesn't have a clean meta API, we construct the direct maven URL.
		// Note: Requires exact forge version string in advanced setups. This assumes standard release format.
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
// SERVER DOWNLOADER (VANILLA, PAPER, FABRIC)
// ---------------------------------------------------------
func dlServer(ver, typ, nam, prt string) {
	fmt.Printf("Constructing Server %s %s on port %s...\n", typ, ver, prt)
	sDir := filepath.Join(serDir, nam)
	os.MkdirAll(sDir, 0755)
	jarPath := filepath.Join(sDir, "server.jar")

	if typ == "vanilla" {
		resp, _ := http.Get("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json")
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
		resp2, _ := http.Get(vURL)
		var vData map[string]interface{}
		json.NewDecoder(resp2.Body).Decode(&vData)
		sURL := vData["downloads"].(map[string]interface{})["server"].(map[string]interface{})["url"].(string)
		dlFile(sURL, jarPath)

	} else if typ == "paper" {
		resp, _ := http.Get(fmt.Sprintf("https://api.papermc.io/v2/projects/paper/versions/%s", ver))
		var pd map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&pd)
		blds := pd["builds"].([]interface{})
		bld := fmt.Sprintf("%.0f", blds[len(blds)-1].(float64))
		dlFile(fmt.Sprintf("https://api.papermc.io/v2/projects/paper/versions/%s/builds/%s/downloads/paper-%s-%s.jar", ver, bld, ver, bld), jarPath)

	} else if typ == "fabric" {
		resp, _ := http.Get("https://meta.fabricmc.net/v2/versions/installer")
		var inst []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&inst)
		instVer := inst[0]["version"].(string)
		instPath := filepath.Join(sDir, "fabric-installer.jar")
		dlFile(fmt.Sprintf("https://maven.fabricmc.net/net/fabricmc/fabric-installer/%s/fabric-installer-%s.jar", instVer, instVer), instPath)
		exec.Command("java", "-jar", instPath, "server", "-mcversion", ver, "-downloadMinecraft", "-dir", sDir).Run()
		os.Rename(filepath.Join(sDir, "fabric-server-launch.jar"), jarPath)
		os.Remove(instPath)
	}

	ioutil.WriteFile(filepath.Join(sDir, "eula.txt"), []byte("eula=true"), 0644)
	ioutil.WriteFile(filepath.Join(sDir, "server.properties"), []byte(fmt.Sprintf("server-port=%s", prt)), 0644)
	fmt.Println("Server Ignition Ready.")
}

// ---------------------------------------------------------
// LAUNCHERS & MONITORS
// ---------------------------------------------------------
func startCli(nam string) {
	auth := chkAuth()
	mcDir := filepath.Join(cliDir, nam, "minecraft")

	// Dynamic Profile Discovery (Finds Fabric/Forge/Vanilla JSONs)
	vDir, _ := ioutil.ReadDir(filepath.Join(mcDir, "versions"))
	var activeVer string
	var mainClass string

	// Prioritize modded profiles if they exist in the folder
	for _, d := range vDir {
		if d.IsDir() {
			if strings.Contains(d.Name(), "fabric") || strings.Contains(d.Name(), "forge") || strings.Contains(d.Name(), "neoforge") {
				activeVer = d.Name()
				break
			}
			activeVer = d.Name() // Fallback to vanilla
		}
	}

	bJSON, _ := ioutil.ReadFile(filepath.Join(mcDir, "versions", activeVer, activeVer+".json"))
	var profileData map[string]interface{}
	json.Unmarshal(bJSON, &profileData)
	mainClass = profileData["mainClass"].(string)

	natDir := filepath.Join(mcDir, "versions", strings.Split(activeVer, "-")[0], "natives") // Natives are always tied to base version

	var cpStrings []string
	filepath.Walk(filepath.Join(mcDir, "libraries"), func(p string, info os.FileInfo, err error) error {
		if strings.HasSuffix(p, ".jar") {
			cpStrings = append(cpStrings, p)
		}
		return nil
	})

	// Add base jar
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
// UTILS & AUTH
// ---------------------------------------------------------
func dlMod(url, tgt string) {
	mDir := filepath.Join(cliDir, tgt, "minecraft", "mods")
	if _, err := os.Stat(filepath.Join(serDir, tgt)); err == nil {
		mDir = filepath.Join(serDir, tgt, "mods")
	}
	os.MkdirAll(mDir, 0755)
	dlFile(url, filepath.Join(mDir, filepath.Base(url)))
}

func doDeviceAuth() string {
	req, _ := http.NewRequest("POST", msAuthURL, strings.NewReader(fmt.Sprintf("client_id=%s&scope=XboxLive.signin offline_access", clientID)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := http.DefaultClient.Do(req)
	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	fmt.Printf("\nGo to: %s\nEnter Code: %s\nWaiting...\n", res["verification_uri"], res["user_code"])

	for {
		time.Sleep(time.Duration(res["interval"].(float64)) * time.Second)
		pr, _ := http.NewRequest("POST", "https://login.microsoftonline.com/consumers/oauth2/v2.0/token", strings.NewReader(fmt.Sprintf("grant_type=urn:ietf:params:oauth:grant-type:device_code&client_id=%s&device_code=%s", clientID, res["device_code"])))
		pr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		pres, _ := http.DefaultClient.Do(pr)
		if pres.StatusCode == 200 {
			var t map[string]interface{}
			json.NewDecoder(pres.Body).Decode(&t)
			return t["access_token"].(string)
		}
	}
}

func runFullAuthFlow(msToken string) {
	// XBL -> XSTS -> MC -> Profile chain
	fmt.Println("Executing Xbox Live Handshake...")
	xB, _ := json.Marshal(map[string]interface{}{"Properties": map[string]interface{}{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "d=" + msToken}, "RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT"})
	req, _ := http.NewRequest("POST", "https://user.auth.xboxlive.com/user/authenticate", bytes.NewBuffer(xB))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, _ := http.DefaultClient.Do(req)
	var xbl map[string]interface{}
	json.NewDecoder(res.Body).Decode(&xbl)

	sB, _ := json.Marshal(map[string]interface{}{"Properties": map[string]interface{}{"SandboxId": "RETAIL", "UserTokens": []string{xbl["Token"].(string)}}, "RelyingParty": "rp://api.minecraftservices.com/", "TokenType": "JWT"})
	req2, _ := http.NewRequest("POST", "https://xsts.auth.xboxlive.com/xsts/authorize", bytes.NewBuffer(sB))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "application/json")
	res2, _ := http.DefaultClient.Do(req2)
	var xsts map[string]interface{}
	json.NewDecoder(res2.Body).Decode(&xsts)

	uhs := xsts["DisplayClaims"].(map[string]interface{})["xui"].([]interface{})[0].(map[string]interface{})["uhs"].(string)
	mB, _ := json.Marshal(map[string]interface{}{"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", uhs, xsts["Token"].(string))})
	req3, _ := http.NewRequest("POST", "https://api.minecraftservices.com/authentication/login_with_xbox", bytes.NewBuffer(mB))
	req3.Header.Set("Content-Type", "application/json")
	res3, _ := http.DefaultClient.Do(req3)
	var mc map[string]interface{}
	json.NewDecoder(res3.Body).Decode(&mc)

	req4, _ := http.NewRequest("GET", "https://api.minecraftservices.com/minecraft/profile", nil)
	req4.Header.Set("Authorization", "Bearer "+mc["access_token"].(string))
	res4, _ := http.DefaultClient.Do(req4)
	var prof map[string]interface{}
	json.NewDecoder(res4.Body).Decode(&prof)

	b, _ := json.Marshal(AuthData{UUID: prof["id"].(string), Name: prof["name"].(string), Token: mc["access_token"].(string)})
	ioutil.WriteFile(authF, b, 0644)
	fmt.Printf("Authorization sequence locked. Welcome, %s.\n", prof["name"].(string))
}

func dlFile(url, dest string) {
	r, err := http.Get(url)
	if err == nil {
		defer r.Body.Close()
		f, _ := os.Create(dest)
		defer f.Close()
		io.Copy(f, r.Body)
	}
}

func unzip(src, dest string) {
	r, _ := zip.OpenReader(src)
	defer r.Close()
	for _, f := range r.File {
		path := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, os.ModePerm)
			continue
		}
		rc, _ := f.Open()
		outFile, _ := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
	}
}
