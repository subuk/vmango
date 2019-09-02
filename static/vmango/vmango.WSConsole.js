(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.WSConsole = function(el){
        var loc = window.location,
            $consoleEl = $(el),
            $consoleWindowEl = $consoleEl.find('.JS-WSConsole-Window'),
            $consoleInputFormEl = $consoleEl.find('.JS-WSConsole-InputForm'),
            $consoleInputFieldEl = $consoleInputFormEl.find("input[name='Command']"),
            wsUri;
        if (loc.protocol === "https:") {
            wsUri = "wss:";
        } else {
            wsUri = "ws:";
        }
        wsUri += "//" + loc.host;
        wsUri += $consoleEl.attr('data-JSConsole-WSUrl');
        var socket = new WebSocket(wsUri);
        socket.onopen = function(){
            $consoleWindowEl.text('');
            $consoleWindowEl.text("Connected, send any text to start");
        }
        socket.onmessage = function(event){
            $consoleWindowEl.append(event.data);
            $consoleWindowEl.scrollTop($consoleWindowEl.prop('scrollHeight'));
        }
        socket.onclose = function(){
            $consoleWindowEl.append("\nConnection closed, reconnecting in 3 seconds...\n");
            setTimeout(function(){start(websocketServerLocation)}, 3000);
        };
        $consoleInputFormEl.on("submit", function(){
            socket.send($consoleInputFieldEl.val() + "\n");
            $consoleInputFieldEl.val('');
            return false
        });
    }

})(window);
