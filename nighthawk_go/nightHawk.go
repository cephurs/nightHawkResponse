/*
 *@package  main
 *@file     nightHawk.go
 *@author   roshan maskey <roshanmaskey@gmail.com>
 *@version  0.0.6
 *@updated  2016-06-25
 *
 *@description  nighHawk main execution file
 */

package main

import (
    "flag"
    "runtime"
    "os"
    "os/exec"
    "time"
    "path/filepath"
    "strings"
    "regexp"
    "sync"

    "fmt"
    "nightHawk"
)



type RuntimeOptions struct {
    CaseName        string
    CaseDate        string
    CaseAnalyst     string
    ComputerName    string 
    ConfigFile      string
    Filename        string
    Debug           string
    Version         bool
    Verbose         bool
}


func ExitOnError(errmsg string, errcode int) {
    fmt.Printf("%s - nightHawkTriage - ERROR - %s\n", time.Now().UTC(), errmsg)
    os.Exit(errcode)
}

func ConsoleMessage(level string, message string, verbose bool) {
    nightHawk.ConsoleMessage(level, message, verbose)
}


func main() {
    runtime.GOMAXPROCS(nightHawk.MAXPROCS)

    // Setting commandline argument parser
    var runopt RuntimeOptions

    flag.StringVar(&runopt.CaseName, "N", "", "Case name collected triage. If this value is not supplied system generated case name is used.")
    flag.StringVar(&runopt.ComputerName, "C", "", "Computer name")
    flag.StringVar(&runopt.ConfigFile, "c", nightHawk.CONFIG, "nightHawk configuration file")
    flag.StringVar(&runopt.CaseDate, "D", "", "Case date for collected triage. If this value is not supplied current date is used.")
    flag.StringVar(&runopt.CaseAnalyst, "a", "", "Case analyst working with collected triage")
    flag.StringVar(&runopt.Filename, "f", "", "File containing triage file")
    flag.StringVar(&runopt.Debug, "d", "none", "Specify debug generator to be debugged. For list of available generator use \"-d list\" ")
    flag.BoolVar(&runopt.Version, "V", false, "Display version information")
    flag.BoolVar(&runopt.Verbose, "v", false, "Show verbose message on console")

    flag.Parse()

    if runopt.Version {
        nightHawk.ShowVersion()
        os.Exit(0)
    }

    if runopt.Debug == "list" {
        nightHawk.ShowAuditGenerators()
        os.Exit(0)
    }

    if !nightHawk.LoadConfigFile(runopt.ConfigFile) {
        ExitOnError("Error encounter reading configuration file", nightHawk.ERROR_CONFIG_FILE_READ)
    }

    if runopt.Verbose {
        nightHawk.VERBOSE = true
    }

    if runopt.CaseName == "" {
        runopt.CaseName = nightHawk.GenerateCaseName()
    }

    if runopt.CaseDate == "" {
        runopt.CaseDate = fmt.Sprintf("%s",time.Now().UTC().Format(nightHawk.Layout))
    }

    if runopt.Filename == "" {
        ExitOnError("Triage file must be supplied", nightHawk.ERROR_NO_TRIAGE_FILE)
    }
    // __end_of_commandline_parsing

    var caseinfo = nightHawk.CaseInformation{CaseName: runopt.CaseName, CaseDate: runopt.CaseDate, CaseAnalyst: runopt.CaseAnalyst}

    sourcetype := nightHawk.SourceDataFileType(runopt.Filename)

    if sourcetype == nightHawk.MOD_XML {
        if runopt.ComputerName == "" {
            ExitOnError("Computer Name is requried while processing single audit file", nightHawk.ERROR_AUDIT_COMPUTERNAME_REQUIRED)
        }
        errno := LoadSingleAuditFile(caseinfo, runopt.ComputerName, runopt.Filename)
        
        if errno > 0 {
            ExitOnError("Error occured processing single file", errno)
        }

    } else if sourcetype == nightHawk.MOD_MANS {
        errno := LoadHxAuditFile(caseinfo, runopt.Filename, runopt.Debug)
        
        if errno > 0 {
            ExitOnError("Error occured processing Hx triage file", errno)
        }

    } else if sourcetype == nightHawk.MOD_ZIP {
        errno := LoadRedlineAuditFile(caseinfo, runopt.Filename, runopt.Debug)
        
        if errno > 0 {
            ExitOnError("Error occured processing Redline triage file", errno)
        }

    } else if sourcetype == nightHawk.MOD_REDDIR {
        errno := LoadRedlineAuditDirectory(caseinfo, runopt.Filename, runopt.Debug)
        if errno > 0 {
            ExitOnError("Unsupported source file", errno)
        }
    }

} // __end_main__




func LoadSingleAuditFile(caseinfo nightHawk.CaseInformation, computername string, filename string) int {
    ConsoleMessage("INFO", "Processing single audit file from "+computername, nightHawk.VERBOSE)

    targetdir, auditfile := filepath.Split(filename)

    auditname,_ := AuditGeneratorFromFile(filename)

    data := nightHawk.LoadAuditData(nightHawk.MOD_JSON, computername, caseinfo, targetdir, auditfile)
    rlRecords := data.([]nightHawk.RlJsonRecord)

    SzRlRecord := len(rlRecords)

    cmsg := fmt.Sprintf("Processing %s::%s => %s : %d records\n", computername, auditname, auditfile, SzRlRecord)
    ConsoleMessage("INFO", cmsg, nightHawk.VERBOSE)


    if SzRlRecord > nightHawk.BULKPOST_SIZE {

        /// StartTestBlock
        rCount := SzRlRecord / nightHawk.BULKPOST_SIZE
        
        var wg sync.WaitGroup

        for i := 0; i < rCount+1; i++ {
            wg.Add(1)
            start := i * nightHawk.BULKPOST_SIZE
            stop := start + nightHawk.BULKPOST_SIZE

            if stop > SzRlRecord {
                stop = SzRlRecord
            }

            go FastUpload(&wg, computername, auditname, start, stop, rlRecords)
        }
        wg.Wait()       

    } else {
        var EsRlRecord string
        for _,bdata := range rlRecords {
            EsRlRecord += "{\"index\":{\"_type\":\"audit_type\", \"_parent\":\"" + computername + "\"}}" + "\n" + string(bdata) + "\n"
        }
        nightHawk.ProcessOutput(computername, auditname, []byte(EsRlRecord))
    }

    // Processing ProcessMemory Tree
    if auditname == "w32processes-memory" {
        msg := fmt.Sprintf("Process %s::%s\n", auditname,auditfile)
        ConsoleMessage("INFO", msg, nightHawk.VERBOSE)
        jsonData := nightHawk.CreateProcessTree(caseinfo, computername, filename)
        esData := "{\"index\":{\"_type\":\"audit_type\", \"_parent\":\"" + computername + "\"}}" + "\n" + string(jsonData) + "\n"
        nightHawk.ProcessOutput(computername, auditname, []byte(esData))
    }

    return 0
}





func LoadHxAuditFile(caseinfo nightHawk.CaseInformation, filename string, debugmodule string) int {
    ConsoleMessage("INFO", "Processing mans file", nightHawk.VERBOSE)
    return LoadRedlineAuditFile(caseinfo, filename, debugmodule)
}


func LoadRedlineAuditFile(caseinfo nightHawk.CaseInformation, filename string, debugmodule string) int {
    ConsoleMessage("INFO", "Processing redline file", nightHawk.VERBOSE)

    targetDir := CreateSessionDirectory(filename)
    ConsoleMessage("INFO", "Session directory "+targetDir, nightHawk.VERBOSE)

    // Fix for Redline audit file containing one-level sub folder
    if !IsRedlineAuditDirectory(targetDir) {
        ConsoleMessage("DEBUG", targetDir + " is not Redline Audit directory", nightHawk.VERBOSE)
        dirList, _ := filepath.Glob(filepath.Join(targetDir, "*"))

        for _,d := range dirList {
            if IsRedlineAuditDirectory(d) {
                targetDir = d
                ConsoleMessage("INFO", "Session directory updated to "+targetDir, nightHawk.VERBOSE)
                break
            }
        }
    }
    

    manifest, err := nightHawk.GetAuditManifestFile(targetDir)
    if err != nil {
        panic(err.Error())
    }

    var rlman nightHawk.RlManifest
    rlman.ParseAuditManifest(filepath.Join(targetDir, manifest))
    auditfiles := rlman.Payloads2(targetDir)
    
    computername := rlman.SysInfo.SystemInfo.Machine
    if computername == "" {
        ExitOnError("Failed to get Computer Name from Audits", nightHawk.ERROR_READING_COMPUTERNAME)
    }
    cmsg := fmt.Sprintf("Processing Redline audits for %s\n", computername)
    ConsoleMessage("INFO", cmsg, nightHawk.VERBOSE)

    var rlwg sync.WaitGroup

    for _,auditfile := range auditfiles {
        rlwg.Add(1)
        go GoLoadAudit(&rlwg, computername, caseinfo, targetDir, auditfile)
    }
    rlwg.Wait()
    os.RemoveAll(targetDir)
    return 0
}


func IsRedlineAuditDirectory(dirPath string) bool {
    ConsoleMessage("DEBUG", "Checking if " + dirPath + " is Redline Directory", nightHawk.VERBOSE)

    fList,err := filepath.Glob(filepath.Join(dirPath,"*"))
    if err != nil {
        panic(err.Error())
    }

    if len(fList) <= 5 {
        return false
    }
    
    for _,f := range fList {
        if strings.Contains(f, "manifest") {
            return true
        }
    }

    return false
}


func FilenameToComputerName(filename string) string {
    if strings.Contains(filename, "_mir") {
        return strings.SplitN(filename,"_mir", 2)[0]
    }
    return ""
}


/// Redline Audit folder
func LoadRedlineAuditDirectory(caseinfo nightHawk.CaseInformation, filename string, debugmodule string) int {
    // Check if supplied path is directory
    fd,err := os.Open(filename)
    if err != nil {
        panic(err.Error())
    }
    defer fd.Close()

    finfo,_ := fd.Stat()

    if finfo.Mode().IsRegular() {
        return nightHawk.ERROR_UNSUPPORTED_TRIAGE_FILE
    }

    if !IsRedlineAuditDirectory(filename) {
        return nightHawk.ERROR_UNSUPPORTED_TRIAGE_FILE
    }

    targetDir := filename

    manifest, err := nightHawk.GetAuditManifestFile(targetDir)
    if err != nil {
        panic(err.Error())
    }

    var rlman nightHawk.RlManifest
    rlman.ParseAuditManifest(filepath.Join(targetDir, manifest))
    auditfiles := rlman.Payloads2(targetDir)
    
    computername := rlman.SysInfo.SystemInfo.Machine
    if computername == "" {
        ExitOnError("Failed to get Computer Name from Audits", nightHawk.ERROR_READING_COMPUTERNAME)
    }

    cmsg := fmt.Sprintf("Processing Redline audits for %s\n", computername)
    ConsoleMessage("INFO", cmsg, nightHawk.VERBOSE)

    var rlwg sync.WaitGroup
    

    for _,auditfile := range auditfiles {   
        rlwg.Add(1)
        go GoLoadAudit(&rlwg, computername, caseinfo, targetDir, auditfile)
    }
    rlwg.Wait()
    return 0
}


/// This function will create and extract supplied archive audit files
 /// and returns the full path of the file
 func CreateSessionDirectory(filename string) (string) {
    sessionDir := nightHawk.NewSessionDir(nightHawk.SESSIONDIR_SIZE)
    targetDir := filepath.Join(nightHawk.WORKSPACE, sessionDir)

    os.MkdirAll(targetDir, 0755)

    cmd := exec.Command("unzip", "-q", filename, "-d", targetDir)
    err := cmd.Run()
    if err != nil {
        panic(err.Error())
    }
    return targetDir
 }


 func AuditGeneratorFromFile(auditfile string) (string, string) {
    fd,err := os.Open(auditfile)
    if err != nil {
        panic(err.Error())
    }

    buffer := make([]byte, 500)
    _,err = fd.Read(buffer)
    if err != nil {
        panic(err.Error())
    }

    s := string(buffer)

    re := regexp.MustCompile("generator=\"(.*)\" generatorVersion=\"([0-9.]+)\" ")
    match := re.FindStringSubmatch(s)
    return match[1],match[2]
 }



 func FastUpload(wg *sync.WaitGroup, computername string, auditname string, start int, stop int, RlRecords []nightHawk.RlJsonRecord) {
    defer wg.Done()

    // This block of code is used for debugging if requried
    // and timing test uploading each bulk data
    if nightHawk.VERBOSE && nightHawk.VERBOSE_LEVEL == 7 {
        cmsg := fmt.Sprintf("Initiating %s::%s bulk upload start=>%d end=>%d\n", computername, auditname, start, stop)
        ConsoleMessage("DEBUG", cmsg, nightHawk.VERBOSE)
    }

    var EsRlRecord string 
    for i:=start; i<stop; i++ {
        EsRlRecord += "{\"index\":{\"_type\":\"audit_type\", \"_parent\":\"" + computername + "\"}}" + "\n" + string(RlRecords[i]) + "\n"   
    }

    nightHawk.ProcessOutput(computername, auditname, []byte(EsRlRecord))    

    // This block of code is used for debugging
    if nightHawk.VERBOSE && nightHawk.VERBOSE_LEVEL == 7 {
        cmsg := fmt.Sprintf("Stopping %s::%s bulk upload start=>%d end=>%d\n", computername, auditname, start, stop)
        ConsoleMessage("DEBUG", cmsg, nightHawk.VERBOSE)
    }
 }



 func GoLoadAudit(rlwg *sync.WaitGroup, computername string, caseinfo nightHawk.CaseInformation, targetDir string, auditfile nightHawk.RlAudit) {
    defer rlwg.Done()

    data := nightHawk.LoadAuditData(nightHawk.MOD_JSON, computername, caseinfo, targetDir, auditfile.AuditFile)
    rlRecords := data.([]nightHawk.RlJsonRecord)
    
    SzRlRecord := len(rlRecords)

    msg := fmt.Sprintf("Process %s::%s with %d records\n", auditfile.AuditGenerator, auditfile.AuditFile, SzRlRecord)
    ConsoleMessage("INFO", msg, nightHawk.VERBOSE)

    if SzRlRecord > nightHawk.BULKPOST_SIZE {
        rCount := SzRlRecord / nightHawk.BULKPOST_SIZE
    
        var wg sync.WaitGroup

        for i := 0; i < rCount+1; i++ {
            wg.Add(1)
            start := i * nightHawk.BULKPOST_SIZE
            stop := start + nightHawk.BULKPOST_SIZE

            if stop > SzRlRecord {
                stop = SzRlRecord
            }

            go FastUpload(&wg, computername, auditfile.AuditGenerator, start, stop, rlRecords)
        }
        wg.Wait()   
    } else {
        var EsRlRecord string
        for _,bdata := range rlRecords {
            EsRlRecord += "{\"index\":{\"_type\":\"audit_type\", \"_parent\":\"" + computername + "\"}}" + "\n" + string(bdata) + "\n"
        }
        nightHawk.ProcessOutput(computername, auditfile.AuditGenerator, []byte(EsRlRecord))
    }

    // Processing ProcessMemory Tree
    if auditfile.AuditGenerator == "w32processes-memory" {
        msg = fmt.Sprintf("Process %s::%s\n", nightHawk.PTGenerator, auditfile.AuditFile)
        ConsoleMessage("INFO", msg, nightHawk.VERBOSE)
        jsonData := nightHawk.CreateProcessTree(caseinfo, computername, filepath.Join(targetDir,auditfile.AuditFile))
        esData := "{\"index\":{\"_type\":\"audit_type\", \"_parent\":\"" + computername + "\"}}" + "\n" + string(jsonData) + "\n"
        nightHawk.ProcessOutput(computername, nightHawk.PTGenerator, []byte(esData))
    }
            
 }
