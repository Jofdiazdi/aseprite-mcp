-- Shared by the native extension and the batch runner. No code is accepted
-- from an MCP request: every operation goes through this dispatcher.
local M = { protocol = 1, batch = not app.isUIAvailable }
local docs, counters = {}, {}
local pc = app.pixelColor
local modes = { rgba=ColorMode.RGB, indexed=ColorMode.INDEXED, grayscale=ColorMode.GRAY }
local directions = { forward=AniDir.FORWARD, reverse=AniDir.REVERSE,
                     pingpong=AniDir.PING_PONG, pingpong_reverse=AniDir.PING_PONG_REVERSE }

local function fail(code, message) error({code=code, message=message}, 0) end
local function check(ok, message) if not ok then fail("invalid_argument", message) end end
local function integer(v, lo, hi, name)
  check(type(v)=="number" and v%1==0 and v>=lo and v<=hi,
        (name or "value").." must be an integer from "..lo.." to "..hi)
  return math.tointeger(v)
end
local function array() return {} end
local function mode(s)
  if s.colorMode==ColorMode.RGB then return "rgba" end
  if s.colorMode==ColorMode.GRAY then return "grayscale" end
  return "indexed"
end
local function layer_id(l) return M.batch and tostring(l.uuid) or tostring(l.id) end
local function parent_layer(l) local p=l.parent;if p==l.sprite then return nil end;return p end
local function walk(layers, f)
  for i=1,#layers do local l=layers[i];f(l); if l.isGroup then walk(l.layers,f) end end
end
local function find_layer(s, id)
  local result
  walk(s.layers,function(l) if layer_id(l)==id then result=l end end)
  check(result~=nil,"unknown layer_id: "..tostring(id))
  return result
end
local function current(id)
  local s=docs[id]
  if not s or not s.isValid then docs[id]=nil; fail("document_closed","document is closed; list or open documents again") end
  return s
end
local function palette_at(s, frame)
  local result=s.palettes[1]
  for i=1,#s.palettes do local p=s.palettes[i];if p.frame.frameNumber<=frame then result=p end end
  return result
end
local function pixel(s,c,frame)
  check(c~=nil,"color is required")
  local n=(c.rgba and 1 or 0)+(c.gray_alpha and 1 or 0)+(c.index~=nil and 1 or 0)
  check(n==1,"color must contain exactly one of rgba, gray_alpha, index")
  if s.colorMode==ColorMode.RGB then
    check(c.rgba and #c.rgba==4,"rgba document requires four rgba channels")
    for _,v in ipairs(c.rgba) do integer(v,0,255,"channel") end
    return pc.rgba(c.rgba[1],c.rgba[2],c.rgba[3],c.rgba[4])
  elseif s.colorMode==ColorMode.GRAY then
    check(c.gray_alpha and #c.gray_alpha==2,"grayscale document requires gray_alpha")
    for _,v in ipairs(c.gray_alpha) do integer(v,0,255,"channel") end
    return pc.graya(c.gray_alpha[1],c.gray_alpha[2])
  end
  return integer(c.index,0,#palette_at(s,frame)-1,"palette index")
end
local function transparent(s) return s.colorMode==ColorMode.INDEXED and s.transparentColor or 0 end

-- Hash the supported document state, including raw image bytes, so direct
-- pixel changes by other scripts are detected as well as native undo events.
local function revision(id,s)
  local a,b=1,0
  local function hash(v)
    local str=tostring(v)
    for i=1,#str do a=(a+str:byte(i))%65521; b=(b+a)%65521 end
    a=(a+255)%65521; b=(b+a)%65521
  end
  hash(s.width); hash(s.height); hash(mode(s)); hash(s.transparentColor)
  walk(s.layers,function(l)
    hash(layer_id(l)); hash(l.name); hash(l.stackIndex); hash(l.isVisible); hash(l.isEditable)
    hash(l.opacity); hash(l.blendMode); local parent=parent_layer(l);hash(parent and layer_id(parent) or "")
    if l.isImage then
      for i=1,#s.frames do
        local c=l:cel(i); hash(i)
        if c then hash(c.position.x); hash(c.position.y); hash(c.opacity); hash(c.zIndex)
          hash(c.image.width); hash(c.image.height); hash(c.image.bytes)
        else hash("empty") end
      end
    end
  end)
  for i=1,#s.frames do hash(s.frames[i].duration) end
  for n=1,#s.palettes do local p=s.palettes[n]
    hash(p.frame.frameNumber)
    for i=0,#p-1 do hash(p:getColor(i).rgbaPixel) end
  end
  for i=1,#s.tags do local t=s.tags[i];hash(t.name);hash(t.fromFrame.frameNumber);hash(t.toFrame.frameNumber);hash(t.aniDir) end
  return tostring(counters[id] or 0)..":"..string.format("%08x",(b<<16)|a)
end

function M.attach(id,s)
  if M.batch then s.useLayerUuids=true end
  docs[id]=s
  if not M.batch and not counters[id] then
    counters[id]=0
    local function changed() counters[id]=(counters[id] or 0)+1 end
    for _,ev in ipairs{"change","layername","layeropacity","layervisibility","layerblendmode"} do s.events:on(ev,changed) end
  end
end

local function metadata(id,s)
  local layers,frames,palettes,tags=array(),array(),array(),array()
  walk(s.layers,function(l)
    layers[#layers+1]={layer_id=layer_id(l),name=l.name,
      parent_id=parent_layer(l) and layer_id(parent_layer(l)) or nil,
      kind=l.isGroup and "group" or (l.isTilemap and "tilemap" or "image"),
      visible=l.isVisible,locked=not l.isEditable,opacity=l.opacity}
  end)
  for i=1,#s.frames do local f=s.frames[i];frames[#frames+1]={number=i,duration_ms=math.floor(f.duration*1000+0.5)} end
  for n=1,#s.palettes do local p=s.palettes[n]
    local colors=array()
    for i=0,#p-1 do local c=p:getColor(i); colors[#colors+1]={c.red,c.green,c.blue,c.alpha} end
    palettes[#palettes+1]={frame=p.frame.frameNumber,colors=colors}
  end
  for i=1,#s.tags do local t=s.tags[i]
    local dir="forward";for k,v in pairs(directions) do if v==t.aniDir then dir=k end end
    tags[#tags+1]={name=t.name,from=t.fromFrame.frameNumber,to=t.toFrame.frameNumber,direction=dir}
  end
  return {document_id=id,revision=revision(id,s),width=s.width,height=s.height,color_mode=mode(s),
    modified=s.isModified,path=s.hasAssociatedFile and s.filename or nil,
    layers=layers,frames=frames,palettes=palettes,tags=tags}
end

local function writable(l)
  check(l.isImage and not l.isTilemap,"only ordinary image layers are editable")
  local p=l
  while p do check(p.isEditable,"layer or parent group is locked"); p=parent_layer(p) end
end
local function frame(s,n) return integer(n,1,#s.frames,"frame") end
local function select_sprite(s) app.sprite=s end

-- Native unlink preserves all cel user data, including extension namespaces.
-- Scope the command's timeline range so a user's selection cannot unlink other
-- cels. Restore its selected objects and the active site afterwards.
local function detach(l,f)
  local cel=l:cel(f);if not cel then return nil end
  local linked=false
  for i=1,#l.cels do local c=l.cels[i];if c.frameNumber~=f and c.image.id==cel.image.id then linked=true end end
  if not linked then return cel end
  local selectedLayers,selectedFrames={},{}
  local range=app.range;local kind=range.type
  for i=1,#range.layers do selectedLayers[i]=range.layers[i] end
  for i=1,#range.frames do selectedFrames[i]=range.frames[i] end
  local previousLayer,previousFrame=app.layer,app.frame
  app.layer=l;app.frame=f
  app.range.layers={l};app.range.frames={f}
  local ok,err=pcall(function() app.command.UnlinkCel() end)
  app.layer=previousLayer;app.frame=previousFrame
  if kind==RangeType.EMPTY then app.range.layers={};app.range.frames={}
  elseif kind==RangeType.LAYERS then app.range.frames=selectedFrames;app.range.layers=selectedLayers
  else app.range.layers=selectedLayers;app.range.frames=selectedFrames end
  if not ok then error(err,0) end
  return l:cel(f)
end

-- Work in a detached canvas image. Preserve any existing off-canvas image
-- data when committing the changed visible region.
local function canvas(s,l,f)
  writable(l)
  local old=l:cel(f)
  local img=Image(s.spec);img:clear(transparent(s))
  if old then img:drawImage(old.image,old.position,255,BlendMode.SRC) end
  return img,old
end
local function commit_image(s,l,f,img,old)
  if not old then s:newCel(l,f,img,Point(0,0));return end
  local x,y=math.min(0,old.position.x),math.min(0,old.position.y)
  local right=math.max(s.width,old.position.x+old.image.width)
  local bottom=math.max(s.height,old.position.y+old.image.height)
  check((right-x)*(bottom-y)<=16000000,"cel bounds are too large")
  if x~=0 or y~=0 or right~=s.width or bottom~=s.height then
    local spec=ImageSpec(s.spec);spec.width=right-x;spec.height=bottom-y
    local merged=Image(spec);merged:clear(transparent(s))
    merged:drawImage(old.image,Point(old.position.x-x,old.position.y-y),255,BlendMode.SRC)
    merged:drawImage(img,Point(-x,-y),255,BlendMode.SRC);img=merged
  end
  local c=detach(l,f);c.image=img;c.position=Point(x,y)
end
local function plot(img,x,y,c)
  if x>=0 and y>=0 and x<img.width and y<img.height then img:drawPixel(x,y,c) end
end
local function line(img,x,y,x2,y2,c)
  local dx,dy=math.abs(x2-x),-math.abs(y2-y)
  local sx,sy=x<x2 and 1 or -1,y<y2 and 1 or -1
  local err=dx+dy
  while true do
    plot(img,x,y,c);if x==x2 and y==y2 then break end
    local e=err*2
    if e>=dy then err=err+dy;x=x+sx end
    if e<=dx then err=err+dx;y=y+sy end
  end
end
local draw_ops={pixels=true,rows=true,stroke=true,line=true,rectangle=true,ellipse=true,fill=true,erase=true}

local function draw(s,l,f,e)
  local img,old=canvas(s,l,f)
  local x,y=e.x or 0,e.y or 0
  local c=e.color and pixel(s,e.color,f) or transparent(s)
  if e.op=="pixels" then
    for _,p in ipairs(e.pixels) do plot(img,p.x,p.y,pixel(s,p.color,f)) end
  elseif e.op=="rows" then
    for iy,row in ipairs(e.rows) do
      for ix=1,#row do
        local ch=row:sub(ix,ix)
        if ch=="~" then plot(img,x+ix-1,y+iy-1,transparent(s))
        elseif ch~="." then plot(img,x+ix-1,y+iy-1,pixel(s,e.legend[ch],f)) end
      end
    end
  elseif e.op=="line" then line(img,x,y,e.x2 or 0,e.y2 or 0,c)
  elseif e.op=="stroke" then
    for i,p in ipairs(e.points) do
      local q=e.points[math.max(1,i-1)];line(img,q.x,q.y,p.x,p.y,c)
    end
  elseif e.op=="rectangle" or e.op=="erase" then
    local w,h=e.width,e.height
    if e.op=="erase" then c=transparent(s) end
    for iy=math.max(0,y),math.min(img.height-1,y+h-1) do
      for ix=math.max(0,x),math.min(img.width-1,x+w-1) do
        if e.filled or e.op=="erase" or ix==x or ix==x+w-1 or iy==y or iy==y+h-1 then plot(img,ix,iy,c) end
      end
    end
  elseif e.op=="ellipse" then
    local rx,ry=e.width/2,e.height/2
    local cx,cy=x+rx-0.5,y+ry-0.5
    for iy=math.max(0,y),math.min(img.height-1,y+e.height-1) do
      for ix=math.max(0,x),math.min(img.width-1,x+e.width-1) do
        local inside=((ix-cx)/rx)^2+((iy-cy)/ry)^2<=1
        local edge=not e.filled and (((ix-1-cx)/rx)^2+((iy-cy)/ry)^2>1 or
          ((ix+1-cx)/rx)^2+((iy-cy)/ry)^2>1 or ((ix-cx)/rx)^2+((iy-1-cy)/ry)^2>1 or
          ((ix-cx)/rx)^2+((iy+1-cy)/ry)^2>1)
        if inside and (e.filled or edge) then plot(img,ix,iy,c) end
      end
    end
  elseif e.op=="fill" then
    check(x>=0 and y>=0 and x<img.width and y<img.height,"fill origin outside canvas")
    local target=img:getPixel(x,y)
    if target~=c then
      local queue={{x,y}};local head=1;img:drawPixel(x,y,c)
      while head<=#queue do
        local p=queue[head];queue[head]=false;head=head+1
        for _,d in ipairs{{1,0},{-1,0},{0,1},{0,-1}} do
          local px,py=p[1]+d[1],p[2]+d[2]
          if px>=0 and py>=0 and px<img.width and py<img.height and img:getPixel(px,py)==target then
            img:drawPixel(px,py,c);queue[#queue+1]={px,py}
          end
        end
      end
    end
  end
  commit_image(s,l,f,img,old)
end

local function validate_edits(s,edits)
  check(edits and #edits>0 and #edits<=1000,"provide 1 to 1000 edits")
  local known={}
  for _,e in ipairs(edits) do
    check(type(e.op)=="string","each edit requires op")
    if ({layer_update=true,layer_delete=true,cel_copy=true,cel_move=true,cel_flip=true})[e.op] then
      check(e.layer_id~=nil,"layer_id is required for "..e.op)
      if not known[e.layer_id] then writable(find_layer(s,e.layer_id)) end
    end
    if ({frame_delete=true,frame_move=true,frame_duration=true,cel_copy=true,cel_move=true,cel_flip=true,tag_set=true})[e.op] then
      check(e.frame~=nil,"frame is required for "..e.op)
    end
    if e.duration_ms~=nil then integer(e.duration_ms,1,655350,"duration_ms") end
    if e.op=="frame_duration" then check(e.duration_ms~=nil,"duration_ms is required") end
    if e.op=="frame_move" or e.op=="cel_copy" then integer(e.to_frame,1,1024,"to_frame") end
    if e.opacity~=nil then integer(e.opacity,0,255,"opacity") end
    if e.copy_from~=nil then integer(e.copy_from,1,1024,"copy_from") end
    if e.end_frame~=nil then integer(e.end_frame,1,1024,"end_frame") end
    for _,k in ipairs{"x","y","x2","y2"} do if e[k]~=nil then integer(e[k],-16384,16384,k) end end
    if e.frame~=nil then integer(e.frame,1,1024,"frame") end
    if draw_ops[e.op] then
      check(e.layer_id and e.frame,"drawing requires layer_id and frame")
      if not known[e.layer_id] then writable(find_layer(s,e.layer_id)) end
      if e.op~="pixels" and e.op~="rows" and e.op~="erase" then pixel(s,e.color,math.min(e.frame,#s.frames)) end
      if e.op=="rectangle" or e.op=="ellipse" or e.op=="erase" then
        integer(e.width,1,4096,"width");integer(e.height,1,4096,"height")
      elseif e.op=="pixels" then
        check(e.pixels and #e.pixels<=262144,"pixels must contain at most 262144 entries")
        for _,p in ipairs(e.pixels) do integer(p.x,-16384,16384,"x");integer(p.y,-16384,16384,"y");pixel(s,p.color,math.min(e.frame,#s.frames)) end
      elseif e.op=="rows" then
        check(e.rows and #e.rows>0 and #e.rows<=4096,"rows must contain 1 to 4096 strings")
        local width=#e.rows[1];check(width>0 and width<=4096,"row width must be 1 to 4096")
        for _,row in ipairs(e.rows) do
          check(#row==width,"rows must have equal widths")
          for i=1,#row do local ch=row:sub(i,i)
            if ch~="." and ch~="~" then
              check(e.legend and e.legend[ch],"unknown row symbol: "..ch)
              pixel(s,e.legend[ch],math.min(e.frame,#s.frames))
            end
          end
        end
      elseif e.op=="stroke" then
        check(e.points and #e.points>0 and #e.points<=16384,"stroke requires 1 to 16384 points")
        for _,p in ipairs(e.points) do integer(p.x,-16384,16384,"x");integer(p.y,-16384,16384,"y") end
      end
    elseif e.op=="layer_create" then
      check(e.name and #e.name>0,"layer name required")
      if e.ref then check(not known[e.ref],"duplicate layer ref");known[e.ref]=true end
    elseif not ({layer_update=true,layer_delete=true,frame_create=true,frame_delete=true,frame_move=true,
      frame_duration=true,cel_copy=true,cel_move=true,cel_flip=true,tag_set=true,tag_delete=true,
      palette_set=true,palette_preset=true})[e.op] then fail("invalid_argument","unknown edit op: "..e.op) end
  end
end

local function copy_cel(s,l,from,to)
  writable(l)
  local c=l:cel(from)
  if not c then local dest=l:cel(to);if dest then s:deleteCel(dest) end;return end
  local img=c.image:clone();local pos=c.position;local opacity,z,data=c.opacity,c.zIndex,c.data
  local n=s:newCel(l,to,img,pos);n.opacity=opacity;n.zIndex=z;n.data=data
end
local function apply(s,edits)
  local refs={}
  for _,e in ipairs(edits) do
    local l=e.layer_id and (refs[e.layer_id] or find_layer(s,e.layer_id))
    local f=e.frame and frame(s,e.frame)
    if draw_ops[e.op] then draw(s,l,f,e)
    elseif e.op=="layer_create" then
      l=s:newLayer();l.name=e.name;if e.ref then refs[e.ref]=l end
    elseif e.op=="layer_update" then
      writable(l)
      if e.name~=nil then check(#e.name>0,"layer name required");l.name=e.name end
      if e.visible~=nil then l.isVisible=e.visible end
      if e.opacity~=nil then l.opacity=integer(e.opacity,0,255,"opacity") end
      if e.position~=nil then l.stackIndex=integer(e.position,1,#(l.parent and l.parent.layers or s.layers),"position") end
    elseif e.op=="layer_delete" then writable(l);s:deleteLayer(l)
    elseif e.op=="frame_create" then
      check(#s.frames<1024,"maximum 1024 frames")
      -- Append a blank frame, copying only when explicitly requested.
      local source=e.copy_from and frame(s,e.copy_from)
      local n=s:newEmptyFrame()
      if source then walk(s.layers,function(layer) if layer.isImage and not layer.isTilemap then copy_cel(s,layer,source,n.frameNumber) end end) end
      if e.duration_ms then n.duration=integer(e.duration_ms,1,655350,"duration_ms")/1000 end
    elseif e.op=="frame_delete" then check(#s.frames>1,"cannot delete the last frame");s:deleteFrame(f)
    elseif e.op=="frame_duration" then s.frames[f].duration=integer(e.duration_ms,1,655350,"duration_ms")/1000
    elseif e.op=="frame_move" then
      local to=frame(s,e.to_frame)
      -- Rotate native cels through a temporary empty frame. Moving Cel objects
      -- retains all user data and linked-image relationships without rebuilding
      -- them from pixels. Tags retain their original timeline ranges.
      walk(s.layers,function(layer)
        check(not layer.isTilemap,"frame_move does not support tilemap layers")
        if layer.isImage then writable(layer) end
      end)
      local durations={};for i=math.min(f,to),math.max(f,to) do durations[i]=s.frames[i].duration end
      local tags={};for i=1,#s.tags do local t=s.tags[i];tags[i]={tag=t,first=t.fromFrame.frameNumber,last=t.toFrame.frameNumber} end
      local temporary=s:newEmptyFrame().frameNumber
      walk(s.layers,function(layer)
        if not layer.isImage then return end
        local moving=layer:cel(f);if moving then moving.frame=temporary end
        if f<to then
          for i=f+1,to do local c=layer:cel(i);if c then c.frame=i-1 end end
        elseif f>to then
          for i=f-1,to,-1 do local c=layer:cel(i);if c then c.frame=i+1 end end
        end
        local c=layer:cel(temporary);if c then c.frame=to end
      end)
      s:deleteFrame(temporary)
      for _,t in ipairs(tags) do t.tag.fromFrame=s.frames[t.first];t.tag.toFrame=s.frames[t.last] end
      local function source(i)
        if i==to then return f end
        if f<to then return i+1 else return i-1 end
      end
      for i=math.min(f,to),math.max(f,to) do
        s.frames[i].duration=durations[source(i)]
      end
    elseif e.op=="cel_copy" then
      local first,last=frame(s,e.to_frame),frame(s,e.end_frame or e.to_frame)
      check(first<=last,"to_frame must not exceed end_frame");writable(l)
      -- Snapshot the source before overwriting any destination.
      local c=l:cel(f);local img=c and c.image:clone();local pos=c and c.position
      local opacity=c and c.opacity;local z=c and c.zIndex;local data=c and c.data
      for i=first,last do
        if img then local n=s:newCel(l,i,img,pos);n.opacity=opacity;n.zIndex=z;n.data=data
        else local old=l:cel(i);if old then s:deleteCel(old) end end
      end
    elseif e.op=="cel_move" or e.op=="cel_flip" then
      writable(l);local c=detach(l,f);check(c,"cel is empty")
      if e.op=="cel_move" then c.position=Point(e.x or 0,e.y or 0)
      else check(e.axis=="horizontal" or e.axis=="vertical","axis must be horizontal or vertical")
        local img=c.image:clone();img:flip(e.axis=="horizontal" and FlipType.HORIZONTAL or FlipType.VERTICAL);c.image=img end
    elseif e.op=="tag_set" or e.op=="tag_delete" then
      check(e.name and #e.name>0,"tag name required")
      local tag;for i=1,#s.tags do local t=s.tags[i];if t.name==e.name then tag=t end end
      if e.op=="tag_delete" then check(tag,"tag not found");s:deleteTag(tag)
      else local last=frame(s,e.end_frame or f);check(f<=last,"tag frame range is reversed")
        if not tag then tag=s:newTag(f,last) else tag.fromFrame=s.frames[f];tag.toFrame=s.frames[last] end
        tag.name=e.name;tag.aniDir=directions[e.direction or "forward"] or fail("invalid_argument","unknown animation direction")
      end
    elseif e.op=="palette_set" or e.op=="palette_preset" then
      check(e.colors and #e.colors>0 and #e.colors<=256,"palette requires 1 to 256 RGBA colors")
      local p=Palette(#e.colors)
      for i,c in ipairs(e.colors) do
        check(c.rgba and #c.rgba==4,"palette entries require rgba")
        for _,v in ipairs(c.rgba) do integer(v,0,255,"channel") end
        p:setColor(i-1,Color{r=c.rgba[1],g=c.rgba[2],b=c.rgba[3],a=c.rgba[4]})
      end
      if s.colorMode==ColorMode.INDEXED then
        walk(s.layers,function(layer) if layer.isImage and not layer.isTilemap then
          for i=1,#layer.cels do local c=layer.cels[i];for it in c.image:pixels() do check(it()<#p,"palette would remove an index still in use") end end
        end end)
        check(s.transparentColor<#p,"palette would remove the transparent index")
      end
      s:setPalette(p)
    end
  end
end

local function read_pixels(s,a)
  local f=frame(s,a.frame or 1)
  local r=a.region or {x=0,y=0,width=s.width,height=s.height}
  integer(r.x,-16384,16384,"region.x");integer(r.y,-16384,16384,"region.y")
  integer(r.width,1,4096,"region.width");integer(r.height,1,4096,"region.height")
  check(r.width*r.height<=262144,"read at most 262144 pixels per call")
  local img,ox,oy,result_mode
  if a.layer_id and not a.composite then
    local l=find_layer(s,a.layer_id);check(l.isImage and not l.isTilemap,"not an ordinary image layer")
    local c=l:cel(f);img=c and c.image;ox=c and c.position.x or 0;oy=c and c.position.y or 0;result_mode=mode(s)
  else
    local source=s
    if a.layer_id then
      local original=find_layer(s,a.layer_id)
      local ids={};local targetIndex
      walk(s.layers,function(l) ids[#ids+1]=l;if l==original then targetIndex=#ids end end)
      source=Sprite(s);local cloneLayers={};walk(source.layers,function(l) cloneLayers[#cloneLayers+1]=l end)
      local target=cloneLayers[targetIndex]
      local function isolate(layers)
        local found=false
        for i=1,#layers do local l=layers[i]
          if l==target then l.isVisible=true;found=true
          elseif l.isGroup and isolate(l.layers) then l.isVisible=true;found=true
          else l.isVisible=false end
        end
        return found
      end
      isolate(source.layers)
    end
    img=Image(s.width,s.height,ColorMode.RGB);img:drawSprite(source,f);ox=0;oy=0;result_mode="rgba"
    if source~=s then source:close() end
  end
  local values=array()
  for y=r.y,r.y+r.height-1 do for x=r.x,r.x+r.width-1 do
    local v=result_mode=="indexed" and transparent(s) or 0
    if img and x-ox>=0 and y-oy>=0 and x-ox<img.width and y-oy<img.height then v=img:getPixel(x-ox,y-oy) end
    values[#values+1]=v
  end end
  return {color_mode=result_mode,x=r.x,y=r.y,width=r.width,height=r.height,values=values}
end

function M.dispatch(request)
  local ok,result=pcall(function()
    local a=request.args or {};local op=request.op
    if op=="get_status" then return {protocol=M.protocol,api_version=app.apiVersion,aseprite_version=tostring(app.version)} end
    if op=="list_documents" then
      if not M.batch then
        for i=1,#app.sprites do local s=app.sprites[i]
          local found=false;for _,existing in pairs(docs) do if existing==s then found=true end end
          if not found then M.attach("live-"..s.id,s) end
        end
      end
      local out=array();for id,s in pairs(docs) do if s.isValid then out[#out+1]=metadata(id,s) end end
      return {documents=out}
    end
    if op=="create_document" then
      integer(a.width,1,4096,"width");integer(a.height,1,4096,"height")
      check(a.width*a.height<=1048576,"canvas must contain at most 1048576 pixels")
      check(modes[a.color_mode or "rgba"],"unknown color_mode")
      local s=Sprite(a.width,a.height,modes[a.color_mode or "rgba"])
      M.attach(a.document_id,s);return metadata(a.document_id,s)
    end
    if op=="open_document" then
      local s=app.open(a.path);check(s,"could not open artwork")
      M.attach(a.document_id,s);return metadata(a.document_id,s)
    end
    local id=a.document_id;local s=current(id)
    if op=="get_document" then return metadata(id,s) end
    if op=="read_pixels" then return read_pixels(s,a) end
    if a.revision~=revision(id,s) then fail("stale_revision","document changed; get_document and inspect before retrying") end
    if op=="apply_edits" then
      validate_edits(s,a.edits)
      local previous=app.sprite;select_sprite(s)
      local success,err=pcall(function() app.transaction("MCP: "..(a.label or "Edit artwork"),function() apply(s,a.edits) end) end)
      if previous and previous.isValid then app.sprite=previous end
      if not success then error(err,0) end
      app.refresh();return metadata(id,s)
    elseif op=="undo" or op=="redo" then
      check(not M.batch,"undo and redo require live mode")
      local previous=app.sprite;select_sprite(s)
      if op=="undo" then app.undo() else app.redo() end
      if previous and previous.isValid then app.sprite=previous end
      app.refresh();return metadata(id,s)
    elseif op=="close_document" then
      check(a.discard or not s.isModified,"unsaved changes; save or set discard=true")
      s:close();docs[id]=nil;return {closed=true}
    elseif op=="ack_save" then
      -- The Go server has already committed a validated snapshot using rooted
      -- filesystem access. Mark that exact revision saved in the native editor
      -- using only a private temporary path, then restore its visible filename.
      check(s:saveAs(a.path),"save acknowledgement failed")
      s.filename=a.display_path;return metadata(id,s)
    elseif op=="snapshot" then
      check(s:saveCopyAs(a.path),"snapshot failed");return metadata(id,s)
    elseif op=="export_gif" then
      check(s:saveCopyAs(a.path),"GIF export failed");return {exported=true}
    end
    fail("unsupported_operation","unknown bridge operation: "..tostring(op))
  end)
  if ok then return {id=request.id,ok=true,result=result} end
  if type(result)~="table" or not result.code then result={code="aseprite_error",message=tostring(result)} end
  return {id=request.id,ok=false,error=result}
end

return M
