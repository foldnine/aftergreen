// Package verify evaluates supplied observations against expected outcomes.
// It does not infer success merely because a workflow reports success.
package verify

import (
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "sort"
)

type Event struct {
 ID string `json:"id"`
 ExecutionStatus string `json:"execution_status"`
 Expected map[string]string `json:"expected"`
 Actual map[string]string `json:"actual"`
 ObservationAvailable bool `json:"observation_available"`
}
type Input struct { Events []Event `json:"events"` }
type Result struct {
 ID string `json:"id"`
 ExecutionStatus string `json:"execution_status"`
 Status string `json:"status"`
 Reasons []string `json:"reasons,omitempty"`
}
func Read(path string) (Input,error) {
 var in Input
 b,err:=os.ReadFile(path); if err!=nil{return in,err}
 if err=json.Unmarshal(b,&in);err!=nil{return in,fmt.Errorf("decode evidence: %w",err)}
 if len(in.Events)==0{return in,errors.New("evidence must include at least one event")}
 seen:=map[string]bool{}
 for _,e:=range in.Events {
  if e.ID=="" {return in,errors.New("event ID is required")}
  if seen[e.ID] {return in,fmt.Errorf("duplicate event ID %q",e.ID)}
  seen[e.ID]=true
  if len(e.Expected)==0{return in,fmt.Errorf("event %q has no expectations",e.ID)}
 }
 return in,nil
}
func Evaluate(in Input) []Result {
 out:=make([]Result,0,len(in.Events))
 for _,e:=range in.Events {
  r:=Result{ID:e.ID,ExecutionStatus:e.ExecutionStatus,Status:"VERIFIED"}
  if !e.ObservationAvailable {r.Status="UNKNOWN";r.Reasons=[]string{"destination observation unavailable"};out=append(out,r);continue}
  keys:=make([]string,0,len(e.Expected));for key:=range e.Expected{keys=append(keys,key)};sort.Strings(keys)
  for _,key:=range keys {
   want:=e.Expected[key];got,ok:=e.Actual[key]
   if !ok {r.Reasons=append(r.Reasons,fmt.Sprintf("%s: field missing (expected %q)",key,want))} else if got!=want {r.Reasons=append(r.Reasons,fmt.Sprintf("%s: expected %q, observed %q",key,want,got))}
  }
  if len(r.Reasons)>0 {r.Status="FAILED"}
  out=append(out,r)
 }
 return out
}
