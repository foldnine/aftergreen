package main

import (
 "context"
 "flag"
 "fmt"
 "os"
 "time"

 "github.com/foldnine/aftergreen/internal/httpcheck"
)

func runHTTP(args []string) int {
 f:=flag.NewFlagSet("http",flag.ContinueOnError)
 endpoint:=f.String("url","","HTTP endpoint to verify")
 expectedStatus:=f.Int("status",200,"expected HTTP response status")
 bodyContains:=f.String("contains","","required literal substring in response body (optional)")
 timeout:=f.Duration("timeout",10*time.Second,"request timeout (max 60s)")
 jsonOutput:=f.Bool("json",false,"emit machine-readable JSON verdict")
 if err:=f.Parse(args);err!=nil{return 2}
 if *endpoint==""{fmt.Fprintln(os.Stderr,"error: --url is required");return 2}
 verdict,err:=httpcheck.Verify(context.Background(),httpcheck.Contract{
  URL:*endpoint,ExpectedStatus:*expectedStatus,ExpectedBodySubstring:*bodyContains,Timeout:*timeout,
 })
 if err!=nil{fmt.Fprintln(os.Stderr,"error:",err);return 2}
 if *jsonOutput{fmt.Println(verdict.JSON())}else{
  fmt.Printf("%s  HTTP outcome (%d)\n",verdict.Status,verdict.ObservedStatus)
  for _,reason:=range verdict.Reasons{fmt.Println(" -",reason)}
 }
 switch verdict.Status{case "VERIFIED":return 0;case "FAILED":return 1;default:return 3}
}
