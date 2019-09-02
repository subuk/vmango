(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.WSConsole = function(el){
        var loc = window.location,
            $consoleEl = $(el),
            $consoleWindowEl = $consoleEl.find('.JS-WSConsole-Window'),
            terminal = new Terminal(),
            decoder = new TextDecoder("utf-8", {fatal: false}),
            firstMessage = true,
            socket,
            wsUri;
        if (loc.protocol === "https:") {
            wsUri = "wss:";
        } else {
            wsUri = "ws:";
        }
        wsUri += "//" + loc.host;
        wsUri += $consoleEl.attr('data-JSConsole-WSUrl');
        terminal.off();
        terminal.open($consoleWindowEl[0]);
        terminal.focus();
        terminal.write("Connecting...\r\n");
        socket = new WebSocket(wsUri);
        socket.binaryType = "arraybuffer";
        terminal.onData(function(data){
            socket.send(data);
        })
        socket.onopen = function(){
            terminal.on();
            terminal.write("Connected! Type any key to start\r\n");
        }
        socket.onmessage = function(event){
            if (firstMessage){
                firstMessage = false;
                terminal.clear();
            }
            terminal.write(decoder.decode(event.data, {stream: true}));
        }
        socket.onclose = function(){
            terminal.off();
            terminal.write("Disconnected... Try to reload page to reconnect...\r\n");
        };
    }
})(window);
