package postgres

import (
 "context"
 "database/sql"
 "database/sql/driver"
 "errors"
 "io"
 "strings"
 "testing"
)
type fakeDriver struct{}
type fakeConn struct{}
type fakeTx struct{}
type fakeRows struct{ values [][]driver.Value; index int }
func (fakeDriver) Open(string)(driver.Conn,error){return fakeConn{},nil}
func (fakeConn) Prepare(string)(driver.Stmt,error){return nil,errors.New("unexpected prepare")}
func (fakeConn) Close()error{return nil}
func (fakeConn) Begin()(driver.Tx,error){return fakeTx{},nil}
func (fakeConn) BeginTx(_ context.Context,opts driver.TxOptions)(driver.Tx,error){if !opts.ReadOnly{return nil,errors.New("transaction must be read-only")};return fakeTx{},nil}
func (fakeTx) Commit()error{return nil}
func (fakeTx) Rollback()error{return nil}
func (fakeConn) QueryContext(_ context.Context,q string,args []driver.NamedValue)(driver.Rows,error){
 if !strings.Contains(q,`FROM "public"."leads" WHERE "event_id" = $1 LIMIT 2`){return nil,errors.New("unexpected query")}
 if len(args)!=1{return nil,errors.New("expected parameterized event key")}
 switch args[0].Value {
 case "ok":return &fakeRows{values:[][]driver.Value{{"qualified"}}},nil
 case "bad":return &fakeRows{values:[][]driver.Value{{"pending"}}},nil
 case "missing":return &fakeRows{},nil
 case "duplicate":return &fakeRows{values:[][]driver.Value{{"qualified"},{"qualified"}}},nil
 default:return nil,errors.New("unavailable")
 }
}
func (r *fakeRows) Columns()[]string{return []string{"status"}}
func (r *fakeRows) Close()error{return nil}
func (r *fakeRows) Next(dest []driver.Value)error{
 if r.index>=len(r.values){return io.EOF}
 copy(dest,r.values[r.index]);r.index++;return nil
}
func init() { sql.Register("aftergreen-test-fake", fakeDriver{}) }
func TestOutcomes(t *testing.T){
 db,err:=sql.Open("aftergreen-test-fake","");if err!=nil{t.Fatal(err)};defer db.Close()
 cases:=[]struct{key,want string}{{"ok","VERIFIED"},{"bad","FAILED"},{"missing","FAILED"},{"duplicate","FAILED"},{"offline","UNKNOWN"}}
 for _,tc:=range cases{
  t.Run(tc.key,func(t *testing.T){
   got,err:=Verify(context.Background(),db,Contract{Table:"leads",KeyColumn:"event_id",KeyValue:tc.key,Expected:map[string]string{"status":"qualified"}})
   if err!=nil{t.Fatal(err)}
   if got.Status!=tc.want{t.Fatalf("got %s; want %s",got.Status,tc.want)}
  })
 }
}
func TestRejectSQLIdentifiers(t *testing.T){
 db,_:=sql.Open("aftergreen-test-fake","");defer db.Close()
 _,err:=Verify(context.Background(),db,Contract{Table:"leads;DROP TABLE leads",KeyColumn:"event_id",KeyValue:"ok",Expected:map[string]string{"status":"qualified"}})
 if err==nil{t.Fatal("unsafe identifier accepted")}
}
