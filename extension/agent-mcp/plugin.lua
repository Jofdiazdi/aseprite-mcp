local dispatcher = require("dispatcher")
local socket, status, pairing = nil, "Disconnected", nil

local function disconnect()
  if socket then socket:close();socket=nil end
  status="Disconnected"
end

local function connect(path)
  disconnect()
  local file=assert(io.open(path,"rb"));pairing=json.decode(file:read("*a"));file:close()
  assert(pairing.protocol==dispatcher.protocol,"MCP bridge versions differ; reinstall the extension")
  assert(type(WebSocket)=="table" or type(WebSocket)=="function","Aseprite was built without WebSocket support")
  assert(pairing.url:match("^ws://127%.0%.0%.1:%d+/bridge$"),"pairing endpoint must use loopback")
  socket=WebSocket{url=pairing.url.."?token="..pairing.token,deflate=false,onreceive=function(kind,data)
    if kind==WebSocketMessageType.OPEN then
      status="Connected"
      socket:sendText(json.encode{protocol=dispatcher.protocol,api_version=app.apiVersion,aseprite_version=tostring(app.version)})
    elseif kind==WebSocketMessageType.TEXT then
      local ok,req=pcall(json.decode,data)
      if ok then socket:sendText(json.encode(dispatcher.dispatch(req))) end
    elseif kind==WebSocketMessageType.ERROR or kind==WebSocketMessageType.CLOSE then status="Disconnected" end
  end}
  status="Connecting";socket:connect()
end

function init(plugin)
  if not app.isUIAvailable then return end
  plugin:newCommand{id="AgentMCPConnect",title="Agent MCP: Connect / Status",group="file_scripts",onclick=function()
    local dlg=Dialog("Agent MCP — "..status)
    dlg:label{text="Choose the pairing file printed by aseprite-mcp."}
    dlg:file{id="pairing",label="Pairing file",open=true,filetypes={"json"},filename=plugin.preferences.pairing or ""}
    dlg:button{text="Connect",onclick=function()
      local path=dlg.data.pairing
      local ok,err=pcall(connect,path)
      if ok then plugin.preferences.pairing=path;dlg:close() else app.alert(tostring(err)) end
    end}
    dlg:button{text="Disconnect",onclick=function() disconnect();dlg:close() end}
    dlg:button{text="Close"};dlg:show()
  end}
end

function exit(plugin) disconnect() end
