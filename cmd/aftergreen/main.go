package main

import (
 "encoding/json"
 "flag"
 "fmt"
 "os"
 "path/filepath"

 "github.com/foldnine/aftergreen/internal/verify"
)

const demoJSON = `{
  "events": [
    {"id":"daily-import-001","execution_status":"SUCCESS","observation_available":true,"expected":{"count":"20"},"actual":{"count":"20"}},
    {"id":"lead-sync-182","execution_status":"SUCCESS","observation_available":true,"expected":{"email":"john@example.com","status":"qualified"},"actual":{"email":"","status":"qualified"}},
    {"id":"report-003","execution_status":"SUCCESS","observation_available":false,"expected":{"report":"delivered"},"actual":{}}
  ]
}`
func main(){os.Exit(run(os.Args[1:]))}
func run(args []string) int {
 if len(args)==0 {usage();return 2}
 switch args[0] {
 case "demo":
  f:=flag.NewFlagSet("demo",flag.ContinueOnError);dir:=f.String("dir",".","directory for demo evidence");if f.Parse(args[1:])!=nil{return 2}
  path:=filepath.Join(*dir,"aftergreen-demo.json")
  if err:=os.MkdirAll(*dir,0755);err!=nil{fmt.Fprintln(os.Stderr,err);return 1}
  h,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0644)
  if err!=nil{fmt.Fprintln(os.Stderr,"cannot create demo (will not overwrite):",err);return 1}
  if _,err=h.WriteString(demoJSON+"\n");err!=nil{h.Close();fmt.Fprintln(os.Stderr,err);return 1};if err=h.Close();err!=nil{fmt.Fprintln(os.Stderr,err);return 1}
  fmt.Println("Demo evidence written to",path);fmt.Printf("Run: aftergreen check --file %s\n",path);return 0
 case "check":
  f:=flag.NewFlagSet("check",flag.ContinueOnError);path:=f.String("file","aftergreen-demo.json","path to evidence JSON");jsonOut:=f.Bool("json",false,"emit machine-readable JSON");if f.Parse(args[1:])!=nil{return 2}
  in,err:=verify.Read(*path);if err!=nil{fmt.Fprintln(os.Stderr,"error:",err);return 2}
  results:=verify.Evaluate(in);failed:=false;unknown:=false
  if *jsonOut {b,_:=json.MarshalIndent(results,"","  ");fmt.Println(string(b))}else{
   fmt.Println("AFTERGREEN — Outcome verification")
   for _,r:=range results {fmt.Printf("\n%s  %s (workflow: %s)\n",r.Status,r.ID,r.ExecutionStatus);for _,reason:=range r.Reasons{fmt.Println("  -",reason)}}
  }
  for _,r:=range results{if r.Status=="FAILED"{failed=true};if r.Status=="UNKNOWN"{unknown=true}}
  if failed{return 1};if unknown{return 3};return 0
 case "version":fmt.Println("aftergreen dev (v0.1 preview)");return 0
 default:usage();return 2
 }
}
func usage(){fmt.Fprintln(os.Stderr,"Usage: aftergreen demo [--dir PATH] | check [--file PATH] [--json] | version")}
