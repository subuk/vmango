(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.ReactiveMenu = function(selector){
        var $items = $('li', selector);
        $items.on('click', function(){
            $items.removeClass('active');
            $(this).addClass('active');
            $('#content').hide();
        });
    }
})(window);
