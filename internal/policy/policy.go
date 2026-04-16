package policy

import (
	"regexp"
	"strings"
)

// Verdict is the three-tier decision of the WAND policy engine.
//
// Three outcomes:
//   - BLOCK:     Immediately deny. Action is unambiguously destructive.
//   - CHALLENGE: Risky but potentially legitimate. Agent must halt and
//                request explicit user re-approval before proceeding.
//   - ALLOW:     No dangerous pattern matched. Safe to execute.
type Verdict struct {
	Decision string // "BLOCK", "CHALLENGE", "ALLOW"
	Reason   string
	Pattern  string // the matched pattern (empty for ALLOW)
	Category string // the category of the matched pattern
}

// rule represents a single policy rule with its compiled regex.
type rule struct {
	pattern  *regexp.Regexp
	raw      string // human-readable pattern description
	category string
}

// compileRules turns a map of category→patterns into compiled rules.
func compileRules(patterns map[string][]string) []rule {
	var rules []rule
	for category, pats := range patterns {
		for _, p := range pats {
			re := regexp.MustCompile("(?i)" + p)
			rules = append(rules, rule{
				pattern:  re,
				raw:      p,
				category: category,
			})
		}
	}
	return rules
}

// hardBlockRules are compiled once at init time for zero-allocation matching.
var hardBlockRules []rule

// challengeRules are compiled once at init time.
var challengeRules []rule

func init() {
	hardBlockRules = compileRules(hardBlockPatterns)
	challengeRules = compileRules(challengePatterns)
}

// hardBlockPatterns: unambiguously destructive operations.
// Regexes use word boundaries (\b) and command-context anchoring
// to avoid false positives on natural language.
var hardBlockPatterns = map[string][]string{

	"filesystem_destruction": {
		`\brm\s+-(r|f|rf|fr)\b`,          // rm -rf, rm -f, rm -r
		`\brm\s+--force\b`,               // rm --force
		`\brm\s+--recursive\b`,           // rm --recursive
		`\brmdir\s+`,                     // rmdir <path>
		`\bunlink\s+`,                    // unlink <file>
		`\bshred\s+`,                     // shred <file>
		`\bsrm\s+`,                       // secure remove
		`\bwipe\s+(/|~|\.)`,              // wipe targeting paths
	},

	"disk_destruction": {
		`\bdd\s+if=`,                     // dd if=
		`\bmkfs\b`,                       // mkfs (make filesystem)
		`\bformat\s+c:`,                  // format c:
		`\bfdisk\s+/`,                    // fdisk /dev
		`\bparted\s+/`,                   // parted /dev
		`\bwipefs\s+`,                    // wipefs
		`\bblkdiscard\s+`,               // blkdiscard
	},

	"shell_bombs": {
		`:\(\)\s*\{[^}]*\}\s*;`,          // fork bomb :(){ :|:& };:
		`\beval\s*\(`,                    // eval() — code injection
		`\bexec\s*\(`,                    // exec() — code injection
		`\bmv\s+/\s+/dev/null`,           // mv / /dev/null
		`\bmv\s+/\*\s+/dev/null`,         // mv /* /dev/null
	},

	"process_killing": {
		`\bkillall\s+`,                   // killall <name>
		`\bpkill\s+`,                     // pkill <pattern>
		`\bkill\s+-(9|KILL|SIGKILL)\b`,   // kill -9, kill -KILL
		`\bxkill\b`,                      // xkill
		`\btaskkill\s+/f\b`,              // taskkill /f
	},

	"service_disruption": {
		`\bshutdown\b.*(-h|now|0)`,       // shutdown -h now
		`\breboot\b`,                     // reboot
		`\bpoweroff\b`,                   // poweroff
		`\bhalt\b`,                       // halt
		`\bsystemctl\s+(stop|disable)\s+`,  // systemctl stop/disable
		`\bservice\s+\S+\s+stop\b`,       // service <x> stop
		`\binit\s+[06]\b`,                // init 0 or init 6
	},

	"firewall_destruction": {
		`\bufw\s+disable\b`,              // ufw disable
		`\biptables\s+(-F|--flush)\b`,    // iptables -F
		`\bnft\s+flush\b`,               // nft flush
		`\bfirewall-cmd\s+--panic-on\b`,  // firewall-cmd --panic-on
	},

	"python_destructive": {
		`\bshutil\.rmtree\s*\(`,          // shutil.rmtree()
		`\bos\.remove\s*\(`,              // os.remove()
		`\bos\.unlink\s*\(`,              // os.unlink()
		`\bos\.system\s*\(\s*['"]rm\b`,   // os.system('rm ...')
		`\bos\.rmdir\s*\(`,              // os.rmdir()
		`\bos\.removedirs\s*\(`,          // os.removedirs()
		`subprocess\.call\s*\(\s*\[.*\brm\b`, // subprocess.call(['rm'...])
		`pathlib.*\.unlink\s*\(`,         // pathlib unlink
		`pathlib.*\.rmdir\s*\(`,          // pathlib rmdir
	},

	"nodejs_destructive": {
		`\bfs\.(unlink|rmdir|rmsync|unlinksync)\s*\(`, // fs.unlink(), etc.
		`\bfs\.rm\s*\(`,                   // fs.rm()
		`\bfs\.promises\.rm\s*\(`,         // fs.promises.rm()
		`child_process\.exec\s*\(.*\brm\b`, // child_process.exec('rm')
		`\brimraf\s*\(`,                   // rimraf()
	},

	"ruby_destructive": {
		`\bFileUtils\.rm_rf\b`,           // FileUtils.rm_rf
		`\bFileUtils\.remove_dir\b`,      // FileUtils.remove_dir
		`\bFile\.delete\s*\(`,            // File.delete()
		`\bDir\.delete\s*\(`,             // Dir.delete()
	},

	"sql_destructive": {
		`\bDROP\s+(TABLE|DATABASE|SCHEMA|INDEX|VIEW|TRIGGER|PROCEDURE|FUNCTION)\b`, // DROP TABLE, etc.
		`\bTRUNCATE\s+(TABLE\s+)?\w`,     // TRUNCATE TABLE
		`\bALTER\s+(TABLE|DATABASE)\s+\w`, // ALTER TABLE/DATABASE
	},

	"sql_injection": {
		`['"];?\s*(DROP|DELETE|UPDATE|ALTER|INSERT)\b`,  // '; DROP TABLE
		`\bUNION\s+(ALL\s+)?SELECT\b`,    // UNION SELECT
		`\bOR\s+['"]?1['"]?\s*=\s*['"]?1['"]?`, // OR 1=1, OR '1'='1'
		`;\s*--\s`,                        // ; -- (comment injection)
		`['"]?\s+OR\s+['"][^'"]*['"]\s*=\s*['"]`, // ' OR ''='
	},

	"git_destructive": {
		`\bgit\s+clean\s+-(fd|fdx)\b`,    // git clean -fd/-fdx
		`\bgit\s+checkout\s+--\s+\.`,      // git checkout -- .
		`\bgit\s+restore\s+\.\s*$`,        // git restore .
	},

	"cloud_destruction": {
		`\baws\s+s3\s+(rm|rb)\b`,          // aws s3 rm/rb
		`\baws\s+ec2\s+terminate\b`,       // aws ec2 terminate
		`\baws\s+rds\s+delete\b`,          // aws rds delete
		`\baws\s+lambda\s+delete\b`,       // aws lambda delete
		`\baws\s+cloudformation\s+delete-stack\b`,
		`\bgcloud\s+.*\bdelete\b`,         // gcloud ... delete
		`\baz\s+(group|vm|storage\s+account)\s+delete\b`, // az group delete
		`\bterraform\s+destroy\b`,         // terraform destroy
		`\bkubectl\s+delete\s+(namespace|--all|pod\s+--all|deployment)\b`,
		`\bhelm\s+(uninstall|delete)\b`,   // helm uninstall/delete
	},

	"container_destruction": {
		`\bdocker\s+rm\s+-f\b`,            // docker rm -f
		`\bdocker\s+rmi\s+-f\b`,           // docker rmi -f
		`\bdocker\s+system\s+prune\s+-a\b`, // docker system prune -a
		`\bdocker\s+(container|volume|network)\s+prune\b`,
	},

	"reverse_shell": {
		`\bcurl\s+.*\|\s*(ba)?sh\b`,       // curl | bash
		`\bwget\s+.*\|\s*(ba)?sh\b`,       // wget | bash
		`\bnc\s+-e\b`,                     // nc -e (netcat exec)
		`\bnetcat\s+-e\b`,                // netcat -e
		`/dev/tcp/`,                       // bash /dev/tcp/ redirection
		`\bbash\s+-i\b`,                   // bash -i (interactive)
		`\bexec\s+/bin/(ba)?sh\b`,         // exec /bin/sh
		`python\s+-c\s+['"]import\s+socket`, // python reverse shell
		`perl\s+-e\s+['"]use\s+socket`,    // perl reverse shell
		`\bsocat\s+exec:`,                // socat exec:
		`\bmsfvenom\b`,                   // metasploit payload gen
		`\bmetasploit\b`,                 // metasploit
	},

	"credential_theft": {
		`\bcat\s+~/\.ssh/`,                // cat ~/.ssh/
		`\bcat\s+~/\.aws/`,                // cat ~/.aws/
		`\bcat\s+.*/\.env\b`,             // cat <path>/.env
		`\bcat\s+.*credentials\.json\b`,   // cat credentials.json
		`\bcat\s+.*service-account`,       // cat service-account
		`\bcat\s+/etc/shadow\b`,           // cat /etc/shadow
		`\bcat\s+/etc/passwd\b`,           // cat /etc/passwd
		`\baws\s+sts\s+get-session-token\b`,
		`\bgcloud\s+auth\s+print-access-token\b`,
	},

	"privilege_escalation": {
		`\bchmod\s+(-[rR]\s+)?777\b`,      // chmod 777 / chmod -R 777
		`\bchown\s+(-[rR]\s+)?root\b`,     // chown root / chown -R root
		`\bvisudo\b`,                      // visudo
		`\busermod\s+.*-[aA][gG]\s+sudo\b`, // usermod -aG sudo
		`\bsu\s+(root|-)\s*$`,             // su root / su -
		`\bsetuid\b`,                      // setuid
		`\bsetgid\b`,                      // setgid
	},

	"supply_chain": {
		`\bpip\s+install\s+--(pre|index-url)\b`, // pip install --pre/--index-url
		`\bnpm\s+install\s+--registry\b`,   // npm install from custom registry
		`\bcurl\s+.*\|\s*pip\b`,            // curl | pip
		`\bwget\s+.*\|\s*pip\b`,            // wget | pip
	},

	"ransomware": {
		`\bopenssl\s+enc\s+-aes`,          // openssl bulk encryption
		`\bgpg\s+--encrypt\b`,            // gpg bulk encryption
		`\b7z\s+a\s+-p`,                  // 7z password-protect
		`\bzip\s+-e\b`,                   // zip with password
	},

	"env_manipulation": {
		`\bexport\s+PATH\s*=`,             // export PATH=  (hijack)
		`\bexport\s+LD_PRELOAD\b`,         // export LD_PRELOAD (injection)
		`\bexport\s+LD_LIBRARY_PATH\b`,    // export LD_LIBRARY_PATH
		`\bunset\s+(PATH|HOME)\b`,         // unset PATH / unset HOME
	},

	"cron_manipulation": {
		`\bcrontab\s+-r\b`,                // crontab -r (delete all cron jobs)
		`\bschtasks\s+/(delete|create)\b`, // schtasks /delete or /create
	},

	"log_tampering": {
		`\btruncate\s+-s\s+0\s+/var/log`,  // truncate -s 0 /var/log
		`\bcat\s+/dev/null\s*>\s*/var/log`, // cat /dev/null > /var/log
		`\bshred\s+/var/log`,              // shred /var/log
		`\brm\s+.*/var/log`,              // rm /var/log
	},

	"kernel_tampering": {
		`\binsmod\s+`,                     // insmod (insert kernel module)
		`\brmmod\s+`,                      // rmmod (remove kernel module)
		`\bmodprobe\s+-r\b`,               // modprobe -r (remove module)
		`\bsysctl\s+-w\b`,                // sysctl -w (write kernel param)
		`echo\s+\d+\s*>\s*/proc/`,         // echo 1 > /proc/
		`\bmount\s+-o\s+remount\b`,        // mount -o remount
	},

	"shell_redirect_destructive": {
		`>\s*/dev/(sd|nvme|hd|vd)`,        // redirect to block device
		`>\s*/etc/`,                        // redirect to /etc/
		`>\s*~/`,                           // redirect to home dir file
		`>\s*/var/`,                        // redirect to /var/
		`>\s*/tmp/.*\.\w+\s*&&`,           // redirect-and-chain attack
	},
}

// challengePatterns: risky but potentially legitimate operations.
// The agent must halt and request explicit user re-approval.
var challengePatterns = map[string][]string{

	"git_risky": {
		`\bgit\s+push\s+(-f|--force)\b`,   // git push --force
		`\bgit\s+reset\s+--hard\b`,        // git reset --hard
		`\bgit\s+reset\s+--mixed\b`,       // git reset --mixed
		`\bgit\s+rebase\b`,               // git rebase
		`\bgit\s+branch\s+-[dD]\b`,        // git branch -d/-D
		`\bgit\s+stash\s+(drop|clear)\b`,  // git stash drop/clear
		`\bgit\s+filter-branch\b`,         // git filter-branch
	},

	"sudo_usage": {
		`\bsudo\s+`,                       // sudo <anything>
	},

	"database_admin": {
		`\bGRANT\s+`,                      // GRANT
		`\bREVOKE\s+`,                     // REVOKE
		`\bCREATE\s+(USER|ROLE|DATABASE)\b`, // CREATE USER/ROLE/DATABASE
		`\bDROP\s+(USER|ROLE)\b`,          // DROP USER/ROLE
		`\bALTER\s+SYSTEM\b`,             // ALTER SYSTEM
	},

	"package_management": {
		`\bpip\s+install\s+`,              // pip install
		`\bnpm\s+install\s+`,              // npm install
		`\byarn\s+add\s+`,                // yarn add
		`\bapt(-get)?\s+install\s+`,       // apt install
		`\bbrew\s+install\s+`,             // brew install
		`\bcargo\s+install\s+`,            // cargo install
		`\bgo\s+install\s+`,              // go install
		`\bgem\s+install\s+`,             // gem install
		`\bapt(-get)?\s+remove\s+`,        // apt remove
		`\bpip\s+uninstall\s+`,            // pip uninstall
		`\bnpm\s+uninstall\s+`,            // npm uninstall
	},

	"file_permission_changes": {
		`\bchmod\s+[0-7]{3,4}\s+`,         // chmod 644 <file> (specific modes)
		`\bchown\s+\w+:\w+\s+`,            // chown user:group <file>
		`\bchgrp\s+\w+\s+`,               // chgrp <group> <file>
	},

	"network_operations": {
		`\bcurl\s+-[^|]*\s+https?://`,     // curl to a URL (non-pipe)
		`\bwget\s+-[^|]*\s+https?://`,     // wget to a URL (non-pipe)
		`\bssh\s+\w+@`,                   // ssh user@host
		`\bscp\s+`,                       // scp
		`\brsync\s+`,                     // rsync
	},

	"docker_operations": {
		`\bdocker\s+run\b`,                // docker run
		`\bdocker\s+exec\b`,              // docker exec
		`\bdocker\s+build\b`,             // docker build
		`\bdocker\s+push\b`,              // docker push
		`\bdocker(-|\s)compose\s+up\b`,    // docker-compose up
	},

	"service_management": {
		`\bsystemctl\s+(restart|start)\s+`, // systemctl restart/start
		`\bservice\s+\S+\s+(restart|start)\b`, // service <x> restart/start
	},

	"large_scale_file_ops": {
		`\bmv\s+/\w`,                      // mv /<path> (moving from root)
		`\bcp\s+-[rR]\s+/`,               // cp -r /<path>
	},

	"process_management": {
		`\bkill\s+\d+`,                    // kill <pid>
		`\bkill\s+-\d+\s+`,               // kill -<signal>
	},

	"system_path_writes": {
		`\becho\s+.*>\s*/etc/`,             // echo > /etc/
		`\btee\s+/etc/`,                   // tee /etc/
		`\bcp\s+.*\s+/usr/(local/)?bin/`,  // copy to bin dirs
	},

	"crontab_edit": {
		`\bcrontab\s+-e\b`,                // crontab -e (edit, not delete)
	},
}

// Evaluate performs a deterministic, sub-millisecond check to classify an action.
//
// The function checks three tiers in order:
//  1. Hard blocklist — immediately dangerous patterns → BLOCK
//  2. Challenge list — risky patterns that may be intentional → CHALLENGE
//  3. Default — no pattern matched → ALLOW
//
// All matching uses pre-compiled regexes with word boundaries to prevent
// false positives on natural language and benign code.
func Evaluate(input string) Verdict {
	// Dynamic SQL heuristic: UPDATE/DELETE without WHERE (must look like SQL)
	if isSQLMutation(input) {
		return Verdict{
			Decision: "BLOCK",
			Reason:   "WAND HARD BLOCK: SQL mutation without WHERE clause",
			Pattern:  "SQL UPDATE/DELETE without WHERE",
			Category: "sql_destructive",
		}
	}

	// Tier 1: BLOCK — unambiguously destructive
	for _, r := range hardBlockRules {
		if r.pattern.MatchString(input) {
			return Verdict{
				Decision: "BLOCK",
				Reason:   "WAND HARD BLOCK: " + r.category + " — pattern '" + r.raw + "'",
				Pattern:  r.raw,
				Category: r.category,
			}
		}
	}

	// Tier 2: CHALLENGE — risky but potentially legitimate
	for _, r := range challengeRules {
		if r.pattern.MatchString(input) {
			return Verdict{
				Decision:  "CHALLENGE",
				Reason:    "WAND CHALLENGE: " + r.category + " — pattern '" + r.raw + "' requires explicit user re-approval",
				Pattern:   r.raw,
				Category:  r.category,
			}
		}
	}

	// Tier 3: ALLOW
	return Verdict{
		Decision: "ALLOW",
		Reason:   "Action passed deterministic policy evaluation — no dangerous patterns detected",
	}
}

// isSQLMutation detects UPDATE/DELETE statements without WHERE clauses.
// Only triggers when the input looks like actual SQL (has SQL keywords in
// command position), not natural language containing the word "update".
func isSQLMutation(input string) bool {
	upper := strings.ToUpper(strings.TrimSpace(input))

	// UPDATE <table> SET ... (without WHERE)
	updateRe := regexp.MustCompile(`(?i)\bUPDATE\s+\w+\s+SET\b`)
	if updateRe.MatchString(input) && !strings.Contains(upper, "WHERE") {
		return true
	}

	// DELETE FROM <table> (without WHERE)
	deleteRe := regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+\w+`)
	if deleteRe.MatchString(input) && !strings.Contains(upper, "WHERE") {
		return true
	}

	return false
}

// PatternCount returns the total number of compiled rules for diagnostics.
func PatternCount() (block int, challenge int) {
	return len(hardBlockRules), len(challengeRules)
}
