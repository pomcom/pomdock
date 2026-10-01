# Tools

Baked into the Kali image by `setup-pentest.sh` (the single source of truth: `PENTEST_APT`,
`PENTEST_GO`, `PENTEST_BINS`, `PENTEST_PIP`). Edit those arrays and run `pomdock docker build`.

**Recon & DNS**
subfinder, assetfinder, waybackurls, dnsx, alterx, asnmap, mapcidr, uncover, whois, arp-scan, nbtscan, onesixtyone

**Port scanning**
nmap, ncat, masscan, naabu, rustscan

**Web content discovery**
ffuf, gobuster, feroxbuster, dirb, wfuzz, katana, gospider

**Web scanning & fingerprinting**
nuclei, nikto, whatweb, httpx, meg, kxss, sqlmap, tlsx, cvemap

**Pipeline glue (tomnomnom)**
gf (`~/.gf`: tomnomnom examples + 1ndianl33t/Gf-Patterns for xss/sqli/ssrf/lfi/redirect/...), anew, unfurl, qsreplace, gron, inscope

**Secrets**
gitleaks, trufflehog, jsluice, snallygaster

**SMB / AD / Windows**
smbclient, smbmap, crackmapexec, netexec, enum4linux-ng, ldap-utils, impacket (`python3-impacket`), responder

**Exploitation & cracking**
metasploit-framework, hydra, ncrack, hashcat, john

**Network & MITM**
wireshark-common, tcpdump, netcat-traditional, bettercap, dnsutils, traceroute, net-tools

**Screenshots & OOB**
gowitness, interactsh-client

**Wordlists**
wordlists, seclists

**Base**
curl, wget, jq, vim, less, firefox-esr, default-jre
