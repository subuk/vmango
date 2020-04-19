(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.DynamicItemList = function(baseEl){
        var $container = $(".JS-DynamicItemListContainer", baseEl),
            $templates = $("<div></div>"),
            initTpl = $(baseEl).data("init-tpl") || 0,
            initCount = $(baseEl).data("init-count") || 0;

        $(".JS-DynamicItemListRemove", baseEl).on("click", function(e){
            $(e.target).parents(".JS-DynamicItemListItem").first().remove();
        });

        $(".JS-DynamicItemListTemplate", baseEl).detach().removeClass("JS-DynamicItemListTemplate").show().appendTo($templates);

        $(".JS-DynamicItemListAdd", baseEl).on("click", function(e){
            var templateId = $(e.target).data("template-id");
            var $el = $("#"+templateId, $templates).clone(true);
            if ($container.find(".JS-DynamicItemListItem").length > 0) {
                $container.find(".JS-DynamicItemListItem").last().after($el);
            } else {
                $container.prepend($el);
            }

        });
        for (let idx = 0; idx < initCount; idx++) {
            var $el;
            if (!initTpl) {
                $el = $templates.find(":first-child").first().clone(true);
            } else {
                $el = $templates.find("#"+initTpl).clone(true);
            }
            $container.prepend($el)
        }
    }
})(window);
