local dispatcher=dofile(app.params.dispatcher)
dispatcher.batch=true
local input=assert(io.open(app.params.request,"rb"))
local req=json.decode(input:read("*a"));input:close()
if app.params.input then
  local s=assert(app.open(app.params.input),"cannot open working copy")
  dispatcher.attach(req.args.document_id,s)
end
local reply=dispatcher.dispatch(req)
if reply.ok and app.params.output and req.op~="close_document" then
  local snapshot=dispatcher.dispatch{id="snapshot",op="snapshot",args={
    document_id=req.args.document_id,revision=reply.result.revision or req.args.revision,path=app.params.output}}
  -- Reads return pixel data rather than document metadata. Snapshotting is only
  -- requested by the Go backend for document mutations.
  if not snapshot.ok then reply=snapshot;reply.id=req.id end
  if snapshot.ok then
    -- ASE serialization can trim empty cels and normalize image bounds. Return
    -- the revision of the actual working copy, not the pre-serialization state.
    local canonical=assert(app.open(app.params.output),"cannot reopen working copy")
    dispatcher.attach(req.args.document_id,canonical)
    reply=dispatcher.dispatch{id=req.id,op="get_document",args={document_id=req.args.document_id}}
  end
end
local out=assert(io.open(app.params.response,"wb"));out:write(json.encode(reply));out:close()
